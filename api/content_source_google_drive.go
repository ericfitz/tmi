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
// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: content source that fetches files from Google Drive via service account
type GoogleDriveSource struct {
	service             *drive.Service
	serviceAccountEmail string
}

// NewGoogleDriveSource creates a new GoogleDriveSource from a credentials JSON file.
// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: build a GoogleDriveSource authenticated from a service account credentials file
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
// SEM@f2e01937e40c91e87ac47a34d11870fde716d093: return the provider name "google-drive" (pure)
func (s *GoogleDriveSource) Name() string { return ProviderGoogleDrive }

// CanHandle returns true for docs.google.com and drive.google.com URIs.
// SEM@3d1c365886b95c6bdb2dab7691650f26dd8e27e2: report whether a URI belongs to docs.google.com or drive.google.com (pure)
func (s *GoogleDriveSource) CanHandle(_ context.Context, uri string) bool {
	lower := strings.ToLower(uri)
	host := extractHost(lower)
	return host == googleHostDocs || host == googleHostDrive
}

// Fetch fetches the content of the Google Drive file identified by uri.
// Google Workspace documents (Docs, Sheets, Slides) are exported as OOXML
// (DOCX, XLSX, PPTX) so the higher-fidelity OOXML extractors can parse
// structured content (tables, headings, formatting) rather than the lossy
// text/plain or text/csv export formats. Binary files are downloaded directly.
// SEM@7231febccdb44b858ff3622e6e6bc81ac0ebb575: fetch a Google Drive file, exporting Workspace documents as OOXML
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
		return s.exportFile(ctx, fileID, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case "application/vnd.google-apps.spreadsheet":
		return s.exportFile(ctx, fileID, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	case "application/vnd.google-apps.presentation":
		return s.exportFile(ctx, fileID, "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	default:
		return s.downloadFile(ctx, fileID, file.MimeType)
	}
}

// ValidateAccess checks whether the service account can access the file without downloading it.
// A Drive API error (e.g., 403 Forbidden, 404 Not Found) is treated as "not accessible"
// rather than an application error — the caller should check the bool result.
// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: check whether the service account can access a Google Drive file without downloading it
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
// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: log that the document owner must share the Drive file with the service account
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

// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: export a Google Workspace file to the given MIME type and return its bytes
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

// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: download a binary Google Drive file and return its bytes and MIME type
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
// SEM@1b4dd947b81f4574ca97fa5898daa7620731ab60: parse a Google Drive file ID from a Docs, Drive, or sharing URL (pure)
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
