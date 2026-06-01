//go:build darwin 

package main
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// response and error definitions
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// Request Definitions
type CreateJobRequest struct {
	URL     string `json:"url"`
	Bitrate string `json:"bitrate"`
}
type CreateJobResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	SourceType string `json:"source_type"`
}

const (
	addr        = "127.0.0.1:8080"
	downloads   = "downloads"
	maxBodySize = 1 << 20 // 1MB
)

var page = template.Must(template.New("index").Parse(
	`
<!doctype HTML>
<html>
<head>
<meta charset="UTF-8">
<title>Youtube To MP3</title>
</head>
<body>
	<h1> Youtube to MP3</h1>

	<form method="POST" action="/download">
		<label for="url">YouTube URL</label>
		<input id="url" name="url" type="url" style="width: 500px" required>
		<button type="submit"> download mp3</button>
	</form>
</body>
</html>
	`))

func HandleCreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	rawURL := strings.TrimSpace(r.FormValue("url"))

	if err := validateYoutubeURL(rawURL); err != nil {
		http.Error(w, "must provide a url to a youtube video.", http.StatusBadRequest)
		return
	}

	id, err := randomID()
	if err != nil {
		http.Error(w, "failed to create job", http.StatusInternalServerError)
	}

	outputTemplate := filepath.Join(downloads, id+".%(ext)s")
	expectedMP3 := filepath.Join(downloads, id+".mp3")

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	err = runYTDLP(ctx, rawURL, outputTemplate)
	if err != nil {
		log.Printf("yt-dlp failed: %v", err)
		http.Error(w, "mp3 was not created", http.StatusInternalServerError)
		return
	}

	if _, err := os.Stat(expectedMP3); err != nil {
		log.Printf("expected mp3 is missing: %v", err)
		http.Error(w, "failed to create mp3", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = os.Remove(expectedMP3)
	}()

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Disposition", `attachment; filename="audio.mp3"`)
	http.ServeFile(w, r, expectedMP3)
}

func runYTDLP(ctx context.Context, videoURL string, outputTemplate string) error {
	cmd := exec.CommandContext(
		ctx,
		"yt-dlp",
		"--no-playlist",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--output", outputTemplate,
		videoURL,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w:%s", err, string(output))
	}
	return nil
}
func validateYoutubeURL(raw string) error {
	if raw == "" {
		return errors.New("empty url")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("unsupported scheme")
	}

	host := strings.ToLower(parsed.Hostname())

	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be":
		return nil
	default:
		return errors.New("link must be a youtube link")
	}
}

func randomID() (string, error) {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(b[:]), nil
}
func isAllowedBitrate(bitrate string) bool {
	switch bitrate {
	case "128k", "192k", "256k", "320k":
		return true
	default:
		return false
	}
}
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := page.Execute(w, nil); err != nil {
		http.Error(w, "failed to render page.", http.StatusInternalServerError)
	}
}
func main() {

	if err := os.MkdirAll(downloads, 0755); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("POST /download", HandleCreateJob)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on http://%s", addr)
	log.Fatal(server.ListenAndServe())
}
