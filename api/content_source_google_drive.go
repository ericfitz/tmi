package api

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const googleDriveMaxExportSize = 10 * 1024 * 1024 // 10 MiB

var googleDriveDocPathRegex = regexp.MustCompile(`/(?:document|spreadsheets|presentation|file)/d/([^/]+)`)

// GoogleDriveSource fetches content from Google Drive using a service account.
type GoogleDriveSource struct {
	service             *drive.Service
	serviceAccountEmail string
}

// NewGoogleDriveSource creates a new GoogleDriveSource from a credentials JSON file.
func NewGoogleDriveSource(credentialsFile string, serviceAccountEmail string) (*GoogleDriveSource, error) {
	ctx := context.Background()

	//nolint:gosec // Path comes from operator config, not user input
	creds, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read Google credentials file %s: %w", credentialsFile, err)
	}

	config, err := google.JWTConfigFromJSON(creds, drive.DriveReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Google credentials: %w", err)
	}

	client := config.Client(ctx)
	svc, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create Drive service: %w", err)
	}

	return &GoogleDriveSource{
		service:             svc,
		serviceAccountEmail: serviceAccountEmail,
	}, nil
}

// Name returns the source name.
func (s *GoogleDriveSource) Name() string { return "google_drive" }

// CanHandle returns true for docs.google.com and drive.google.com URIs.
func (s *GoogleDriveSource) CanHandle(_ context.Context, uri string) bool {
	lower := strings.ToLower(uri)
	host := extractHost(lower)
	return host == "docs.google.com" || host == "drive.google.com"
}

// Fetch fetches the content of the Google Drive file identified by uri.
// Google Workspace documents (Docs, Sheets, Slides) are exported as plain text or CSV.
// Binary files are downloaded directly.
func (s *GoogleDriveSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	logger := slogging.Get()

	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok {
		return nil, "", fmt.Errorf("could not extract file ID from Google Drive URL: %s", uri)
	}

	file, err := s.service.Files.Get(fileID).
		Fields("id,name,mimeType").
		Context(ctx).
		Do()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get file metadata: %w", err)
	}

	logger.Debug("GoogleDriveSource: file %s mimeType=%s", file.Name, file.MimeType)

	switch file.MimeType {
	case "application/vnd.google-apps.document":
		return s.exportFile(ctx, fileID, "text/plain")
	case "application/vnd.google-apps.spreadsheet":
		return s.exportFile(ctx, fileID, "text/csv")
	case "application/vnd.google-apps.presentation":
		return s.exportFile(ctx, fileID, "text/plain")
	default:
		return s.downloadFile(ctx, fileID, file.MimeType)
	}
}

// ValidateAccess checks whether the service account can access the file without downloading it.
// A Drive API error (e.g., 403 Forbidden, 404 Not Found) is treated as "not accessible"
// rather than an application error — the caller should check the bool result.
func (s *GoogleDriveSource) ValidateAccess(ctx context.Context, uri string) (bool, error) {
	logger := slogging.Get()

	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok {
		return false, fmt.Errorf("could not extract file ID from Google Drive URL: %s", uri)
	}

	_, err := s.service.Files.Get(fileID).
		Fields("id").
		Context(ctx).
		Do()
	if err != nil {
		logger.Debug("GoogleDriveSource: ValidateAccess file %s not accessible: %v", fileID, err)
		return false, nil
	}
	return true, nil
}

// RequestAccess logs that the document owner should share the file with the service account.
func (s *GoogleDriveSource) RequestAccess(ctx context.Context, uri string) error {
	logger := slogging.Get()

	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok {
		return fmt.Errorf("could not extract file ID from Google Drive URL: %s", uri)
	}

	if s.serviceAccountEmail == "" {
		logger.Warn("GoogleDriveSource: cannot request access — no service account email configured")
		return nil
	}

	logger.Info("GoogleDriveSource: document owner should share file %s with %s", fileID, s.serviceAccountEmail)
	return nil
}

func (s *GoogleDriveSource) exportFile(ctx context.Context, fileID, exportMIME string) ([]byte, string, error) {
	resp, err := s.service.Files.Export(fileID, exportMIME).
		Context(ctx).
		Download()
	if err != nil {
		return nil, "", fmt.Errorf("failed to export file: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read export: %w", err)
	}
	return data, exportMIME, nil
}

func (s *GoogleDriveSource) downloadFile(ctx context.Context, fileID, mimeType string) ([]byte, string, error) {
	resp, err := s.service.Files.Get(fileID).
		Context(ctx).
		Download()
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read download: %w", err)
	}
	return data, mimeType, nil
}

// extractGoogleDriveFileID extracts the Google Drive file ID from a URL.
// It handles /document/d/, /spreadsheets/d/, /presentation/d/, /file/d/ paths,
// as well as drive.google.com/open?id= query parameters.
func extractGoogleDriveFileID(uri string) (string, bool) {
	if matches := googleDriveDocPathRegex.FindStringSubmatch(uri); len(matches) > 1 {
		return matches[1], true
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", false
	}
	if id := parsed.Query().Get("id"); id != "" {
		return id, true
	}

	return "", false
}
