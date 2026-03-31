package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	storageURL  string
	metadataURL string
)

func main() {
	root := &cobra.Command{
		Use:   "filemanage",
		Short: "CLI for the filemanage file storage service",
	}

	root.PersistentFlags().StringVar(&storageURL, "storage-url", "http://localhost:8080", "Storage service base URL")
	root.PersistentFlags().StringVar(&metadataURL, "metadata-url", "http://localhost:8081", "Metadata service base URL")

	root.AddCommand(
		uploadCmd(),
		getCmd(),
		deleteCmd(),
		listCmd(),
		eventsCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// uploadCmd uploads a file and registers its metadata.
func uploadCmd() *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a file to the storage service",
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer f.Close()

			// Build multipart body.
			body := &bytes.Buffer{}
			w := multipart.NewWriter(body)
			part, err := w.CreateFormFile("file", filepath.Base(filePath))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, f); err != nil {
				return err
			}
			w.Close()

			resp, err := http.Post(storageURL+"/upload", w.FormDataContentType(), body)
			if err != nil {
				return fmt.Errorf("upload request: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				msg, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("upload failed (%d): %s", resp.StatusCode, msg)
			}

			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			// Register metadata.
			metaBody, _ := json.Marshal(result)
			mResp, err := http.Post(metadataURL+"/metadata", "application/json", bytes.NewReader(metaBody))
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: metadata registration failed: %v\n", err)
			} else {
				mResp.Body.Close()
			}

			fmt.Printf("Uploaded successfully.\n")
			fmt.Printf("  ID:       %v\n", result["id"])
			fmt.Printf("  Filename: %v\n", result["filename"])
			fmt.Printf("  Size:     %v bytes\n", result["size"])
			fmt.Printf("  Location: %v\n", result["location"])
			return nil
		},
	}
	cmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to the file to upload (required)")
	cmd.MarkFlagRequired("file")
	return cmd
}

// getCmd downloads a file by UUID.
func getCmd() *cobra.Command {
	var id, output string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Download a file by its UUID",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(storageURL + "/file/" + id)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				msg, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("get failed (%d): %s", resp.StatusCode, msg)
			}

			// Determine output path.
			if output == "" {
				output = id
			}
			out, err := os.Create(output)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer out.Close()

			n, err := io.Copy(out, resp.Body)
			if err != nil {
				return err
			}
			fmt.Printf("Downloaded %d bytes → %s\n", n, output)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "File UUID (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: UUID)")
	cmd.MarkFlagRequired("id")
	return cmd
}

// deleteCmd deletes a file by UUID.
func deleteCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a file by its UUID",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, _ := http.NewRequest(http.MethodDelete, storageURL+"/file/"+id, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				msg, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, msg)
			}
			fmt.Printf("Deleted file %s\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "File UUID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}

// listCmd lists all files with their metadata.
func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all files with their metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(metadataURL + "/files/")
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				msg, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("list failed (%d): %s", resp.StatusCode, msg)
			}

			var files []struct {
				ID          string    `json:"id"`
				Filename    string    `json:"filename"`
				Size        int64     `json:"size"`
				ContentType string    `json:"content_type"`
				Location    string    `json:"location"`
				UploadedAt  time.Time `json:"uploaded_at"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
				return err
			}

			if len(files) == 0 {
				fmt.Println("No files found.")
				return nil
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tFILENAME\tSIZE\tCONTENT TYPE\tUPLOADED AT")
			for _, f := range files {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					f.ID, f.Filename, f.Size, f.ContentType, f.UploadedAt.Format(time.RFC3339))
			}
			return tw.Flush()
		},
	}
}

// eventsCmd streams SSE events from the notification service.
func eventsCmd() *cobra.Command {
	var notifURL string
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Stream real-time file events (Ctrl+C to stop)",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(notifURL + "/events")
			if err != nil {
				return fmt.Errorf("connect to notification service: %w", err)
			}
			defer resp.Body.Close()

			fmt.Println("Connected. Listening for events (Ctrl+C to stop)...")
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if line != "" {
					fmt.Println(line)
				}
			}
			return scanner.Err()
		},
	}
	cmd.Flags().StringVar(&notifURL, "notification-url", "http://localhost:8082", "Notification service base URL")
	return cmd
}
