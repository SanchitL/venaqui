package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/mhrsntrk/venaqui/internal/aria2"
	"github.com/mhrsntrk/venaqui/internal/config"
	"github.com/mhrsntrk/venaqui/internal/realdebrid"
	"github.com/mhrsntrk/venaqui/internal/tui"
	"github.com/mhrsntrk/venaqui/internal/utils"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var inputFile string

var rootCmd = &cobra.Command{
	Use:   "venaqui [link] [location]",
	Short: "Download files via Real-Debrid and aria2",
	Long: `Venaqui is a command-line tool with a Terminal User Interface (TUI) that
leverages Real-Debrid premium links and aria2 for high-speed downloads.

Use -i to provide a text file containing multiple links (one per line).
Each link is downloaded sequentially.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if inputFile != "" {
			return nil
		}
		if len(args) < 1 {
			return fmt.Errorf("requires a link argument or -i flag with a file path")
		}
		return nil
	},
	Run: run,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("venaqui version %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built: %s\n", date)
	},
}

func init() {
	rootCmd.Flags().StringVarP(&inputFile, "input", "i", "", "path to a text file containing links (one per line)")
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseBatchFile reads a text file and returns non-empty, non-comment lines.
func parseBatchFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open batch file: %w", err)
	}
	defer f.Close()

	var links []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		links = append(links, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read batch file: %w", err)
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("batch file contains no links")
	}
	return links, nil
}

func run(cmd *cobra.Command, args []string) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please create a config file at ~/.venaqui/config.yaml\n")
		os.Exit(1)
	}

	// Parse arguments
	downloadDir := cfg.DefaultDownloadDir
	if len(args) > 1 {
		downloadDir = args[1]
	} else if inputFile != "" && len(args) > 0 {
		downloadDir = args[0]
	}

	// Normalize and validate download directory
	downloadDir = utils.NormalizePath(downloadDir)
	if err := utils.ValidatePath(downloadDir); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid download directory: %v\n", err)
		os.Exit(1)
	}

	// Ensure download directory exists
	if err := utils.EnsureDirExists(downloadDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create download directory: %v\n", err)
		os.Exit(1)
	}

	// Start aria2c if not running
	if err := ensureAria2Running(cfg.Aria2RPCUrl); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start aria2: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please ensure aria2 is installed and accessible\n")
		os.Exit(1)
	}

	// Determine links to process
	var links []string
	if inputFile != "" {
		links, err = parseBatchFile(inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Batch file loaded: %d link(s) to download\n\n", len(links))
	} else {
		links = []string{args[0]}
	}

	// Process each link sequentially
	for i, link := range links {
		if len(links) > 1 {
			fmt.Printf("━━━ [%d/%d] %s ━━━\n", i+1, len(links), link)
		}

		if err := processLink(cfg, link, downloadDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if i < len(links)-1 {
				fmt.Println("Skipping to next link...")
				continue
			}
			os.Exit(1)
		}

		if len(links) > 1 && i < len(links)-1 {
			fmt.Println()
		}
	}
}

// processLink handles the full download lifecycle for a single link:
// validate → unrestrict via Real-Debrid → download via aria2 → show TUI.
func processLink(cfg *config.Config, link, downloadDir string) error {
	// Validate URL
	if err := utils.ValidateURL(link); err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Initialize Real-Debrid client
	rdClient := realdebrid.NewClient(cfg.RealDebridAPIToken)

	// Validate token
	if err := rdClient.ValidateToken(); err != nil {
		return fmt.Errorf("Real-Debrid API token validation failed: %w", err)
	}

	var unrestrictedLink *realdebrid.UnrestrictedLink
	var filename string

	// Check if this is a torrent or magnet link
	if utils.IsTorrentLink(link) || utils.IsMagnetLink(link) {
		// Handle torrent/magnet link
		fmt.Println("Adding torrent to Real-Debrid...")
		var torrentResp *realdebrid.AddTorrentResponse
		var err error

		if utils.IsMagnetLink(link) {
			torrentResp, err = rdClient.AddMagnet(link)
		} else {
			torrentResp, err = rdClient.AddTorrent(link)
		}

		if err != nil {
			return fmt.Errorf("RD API error: %w", err)
		}

		// Check if files need to be selected
		torrentInfo, err := rdClient.GetTorrentInfo(torrentResp.ID)
		if err != nil {
			return fmt.Errorf("RD API error: %w", err)
		}

		// Select all files if needed
		if torrentInfo.Status == "waiting_files_selection" {
			fileIDs := []int{}
			for _, file := range torrentInfo.Files {
				fileIDs = append(fileIDs, file.ID)
			}
			if len(fileIDs) > 0 {
				fmt.Println("Selecting files...")
				if err := rdClient.SelectFiles(torrentResp.ID, fileIDs); err != nil {
					return fmt.Errorf("RD API error: %w", err)
				}
			}
		}

		fmt.Println("Waiting for torrent to be processed...")
		torrentInfo, err = rdClient.WaitForTorrentReady(torrentResp.ID, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("RD API error: %w", err)
		}

		if len(torrentInfo.Links) == 0 {
			return fmt.Errorf("no download links available from torrent")
		}

		// Get the first link and validate it
		downloadLink := torrentInfo.Links[0]
		if downloadLink == "" {
			return fmt.Errorf("download link is empty")
		}

		// Check if it's already a direct download link (rdeb.io)
		if strings.Contains(downloadLink, "rdeb.io") {
			unrestrictedLink = &realdebrid.UnrestrictedLink{
				Link:     downloadLink,
				Filename: torrentInfo.Filename,
			}
			filename = torrentInfo.Filename
		} else {
			fmt.Println("Unrestricting torrent download link...")
			var err error
			unrestrictedLink, err = rdClient.UnrestrictLink(downloadLink)
			if err != nil {
				return fmt.Errorf("RD API error (link: %s): %w", downloadLink, err)
			}
			filename = torrentInfo.Filename
			if filename == "" {
				filename = unrestrictedLink.Filename
			}
		}
	} else {
		// Handle regular hoster link
		fmt.Println("Unrestricting link via Real-Debrid...")
		var err error
		unrestrictedLink, err = rdClient.UnrestrictLink(link)
		if err != nil {
			return fmt.Errorf("RD API error: %w", err)
		}
		filename = unrestrictedLink.Filename
	}

	// Initialize aria2 client
	aria2Client, err := aria2.NewClient(cfg.Aria2RPCUrl, cfg.Aria2Secret)
	if err != nil {
		return fmt.Errorf("aria2 connection error: %w", err)
	}
	defer aria2Client.Close()

	// Add download to aria2
	downloadURL := unrestrictedLink.Download
	if downloadURL == "" {
		downloadURL = unrestrictedLink.Link
	}
	fmt.Println("Starting download...")
	gid, err := aria2Client.AddDownload(downloadURL, downloadDir)
	if err != nil {
		return fmt.Errorf("download error: %w", err)
	}

	// Start TUI
	if filename == "" {
		filename = unrestrictedLink.Filename
		if filename == "" {
			urlForFilename := downloadURL
			if urlForFilename == "" {
				urlForFilename = unrestrictedLink.Link
			}
			filename = filepath.Base(urlForFilename)
		}
	}

	model := tui.InitialModel(aria2Client, gid, filename)
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

// ensureAria2Running checks if aria2 is running and starts it if needed
func ensureAria2Running(rpcURL string) error {
	// Try to connect to aria2 RPC
	client, err := aria2.NewClient(rpcURL, "")
	if err == nil {
		// Connection successful, check if it's responsive
		if err := client.Ping(); err == nil {
			client.Close()
			return nil
		}
		client.Close()
	}

	// aria2 is not running, try to start it
	fmt.Println("Starting aria2 daemon...")

	cmd := exec.Command("aria2c",
		"--enable-rpc",
		"--rpc-listen-all",
		"--daemon=true",
		"--max-connection-per-server=16",
		"--split=16",
		"--min-split-size=1M",
		"--rpc-allow-origin-all",
	)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start aria2: %w", err)
	}

	// Wait a bit for aria2 to start
	time.Sleep(2 * time.Second)

	// Try to connect again
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		client, err := aria2.NewClient(rpcURL, "")
		if err == nil {
			if err := client.Ping(); err == nil {
				client.Close()
				return nil
			}
			client.Close()
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("aria2 failed to start or is not accessible")
}

// checkPort checks if a port is listening
func checkPort(host, port string) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	if conn != nil {
		conn.Close()
		return true
	}
	return false
}
