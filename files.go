package hoist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"time"
)

const (
	defaultFileType    = "application/octet-stream"
	contextFileStorage = "file-storage"
	maxChunkSize       = 15 * 1024 * 1024 // 15 MB

	apiUpload       = "api/upload"
	apiDiskUsage    = "api/v1/filestorage/disk-usage-summary"
	apiFiles        = "api/v1/filestorage/files"
	apiDeleteFiles  = "api/v1/filestorage/delete-files"
	apiMoveFiles    = "api/v1/filestorage/move-files"
	apiEditFile     = "api/v1/filestorage/{fileId}/edit"
	apiGetFileLink  = "api/v1/filestorage/{fileId}/getlink"
	apiFolder       = "api/v1/filestorage/folder"
	apiFolders      = "api/v1/filestorage/folders"
	apiPutFolder    = "api/v1/filestorage/folder-put"
	apiDeleteFolder = "api/v1/filestorage/delete-folder"
	apiPatchFolder  = "api/v1/filestorage/folder-patch"
	apiFileDownload = "api/v1/filestorage/%s/download"
)

type FileClient interface {
	DiskUsageSummary(ctx context.Context) (*DiskUsage, error)
	ChunkedUpload(ctx context.Context, in io.Reader, filePath string, fileSize int64) (*File, error)
	ParsePath(path string) (basePath, lastSegment string)
	GetFolders(ctx context.Context) ([]Folder, error)
	GetFolder(ctx context.Context, folder string) (*Folder, error)
	GetFiles(ctx context.Context, ids ...string) ([]File, error)
	DeleteFiles(ctx context.Context, ids ...string) error
	DownloadFile(ctx context.Context, id string, opts ...RequestOpt) (io.ReadCloser, error)
	GetFileID(ctx context.Context, dir, fileName string) (string, error)
	Find(ctx context.Context, file string) (*Folder, *File, error)
	CreateFolder(ctx context.Context, folder string) (*Folder, error)
	DeleteFolder(ctx context.Context, folder string) error
	MoveFiles(ctx context.Context, folder string, fileIDs ...string) error
	RenameFile(ctx context.Context, fileID string, name string) error
	EditFile(ctx context.Context, fileID string, params EditFileParams) error
	GetLink(ctx context.Context, fileID string) (string, string, error)
	MoveFolder(ctx context.Context, folder, newParentFolder string) error
}

type diskUsageResponse struct {
	defaultResponse
	DiskUsage *DiskUsage `json:"diskUsage"`
}

type DiskUsage struct {
	Allowed          int64 `json:"allowed"`
	Used             int64 `json:"used"`
	Mailboxes        int64 `json:"mailboxes"`
	Appointments     int64 `json:"appointmentsUsed"`
	Contacts         int64 `json:"contactsUsed"`
	Notes            int64 `json:"notesUsed"`
	Tasks            int64 `json:"tasksUsed"`
	FileStorage      int64 `json:"fileStorageUsed"`
	MeetingWorkspace int64 `json:"meetingWorkspaceUsed"`
	ChatFiles        int64 `json:"chatFilesUsed"`
}

// DiskUsageSummary returns the disk usage information from the API
func (c *client) DiskUsageSummary(ctx context.Context) (*DiskUsage, error) {
	res, err := c.doRequest(ctx, http.MethodGet, apiDiskUsage, nil)

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response diskUsageResponse

	if err := res.Decode(&response); err != nil {
		return nil, err
	}

	// Root folder is response.Folder
	return response.DiskUsage, nil
}

// uploadChunk uploads a chunk, then waits for it to be accepted.
// When the last chunk is uploaded, the backend will combine the file, then return a 200 with a body.
func (c *client) uploadChunk(ctx context.Context, reader io.Reader, fileName string, fileSize, chunkSize int64, fields map[string]string) (*Response, error) {
	// Send POST request to upload
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, fmt.Errorf("failed to write field %s: %w", key, err)
		}
	}

	// Add the file content for this chunk
	part, err := writer.CreateFormFile("file", fileName)

	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err = io.CopyN(part, reader, chunkSize); err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to copy chunk data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	// --- Send the chunk ---

	resp, err := c.doRequest(ctx, http.MethodPost, apiUpload,
		requestBody.Bytes(),
		WithContentType(writer.FormDataContentType()))

	if err != nil {
		return nil, fmt.Errorf("failed to upload file: %w", err)
	}

	return resp, err
}

func (c *client) Upload(ctx context.Context, in io.Reader, filePath string, fileSize int64) (*File, error) {
	return nil, nil
}

// ChunkedUpload will push a file to the client API
func (c *client) ChunkedUpload(ctx context.Context, in io.Reader, filePath string, fileSize int64) (*File, error) {
	fileName := path.Base(filePath)

	// encode brackets, fixing bug within uploader
	//	fileName = url.PathEscape(fileName)

	basePath := path.Dir(filePath)

	if basePath == "" || basePath[0] != '/' {
		basePath = "/" + basePath
	}

	// Prepare context data
	contextBytes, err := json.Marshal(folderRequest{
		Folder: basePath,
	})

	if err != nil {
		return nil, err
	}

	contextData := string(contextBytes)

	// Calculate total chunks
	totalChunks := int(math.Ceil(float64(fileSize) / maxChunkSize))

	remaining := fileSize

	id, err := uuid.NewV7()

	if err != nil {
		return nil, err
	}

	fields := map[string]string{
		"resumableChunkSize":    strconv.FormatInt(maxChunkSize, 10),
		"resumableTotalSize":    strconv.FormatInt(fileSize, 10),
		"resumableIdentifier":   id.String(),
		"resumableType":         defaultFileType,
		"resumableFilename":     fileName,
		"resumableRelativePath": fileName,
		"resumableTotalChunks":  strconv.Itoa(totalChunks),
		"context":               contextFileStorage,
		"contextData":           contextData,
	}

	var res *Response

	for chunk := 1; chunk <= totalChunks; chunk++ {
		chunkSize := int64(maxChunkSize)

		if remaining < maxChunkSize {
			chunkSize = remaining
		}

		// strconv.FormatInt is pretty much fmt.Sprintf but without needing to parse the format, replace things, etc.
		// base 10 is the default, see strconv.Itoa
		fields["resumableChunkNumber"] = strconv.Itoa(chunk)
		fields["resumableCurrentChunkSize"] = strconv.FormatInt(chunkSize, 10)

		// --- Prepare the chunk payload ---
		res, err = c.uploadChunk(ctx, in, fileName, fileSize, chunkSize, fields)

		if err != nil {
			return nil, fmt.Errorf("chunk upload failed, error: %w", err)
		}

		if res.StatusCode != http.StatusOK {
			var status defaultResponse

			if err := res.Decode(&status); err != nil {
				return nil, fmt.Errorf("chunk %d upload failed, status: %d, response: %s", chunk, res.StatusCode, string(res.Data()))
			}

			return nil, fmt.Errorf("chunk %d upload failed, status: %d, message: %s", chunk, res.StatusCode, status.Message)
		}

		if chunk == totalChunks {
			var file File

			if err := res.Decode(&file); err != nil {
				return nil, err
			}

			return &file, nil
		} else {
			_ = res.Close()
		}

		// Update progress
		remaining -= chunkSize
	}

	return nil, errors.New("no response from endpoint")
}

type ListResponse struct {
	Files []File `json:"files"`
}

type FolderResponse struct {
	defaultResponse
	Folder Folder `json:"folder"`
}

// GetFolders returns all folders at the root level
func (c *client) GetFolders(ctx context.Context) ([]Folder, error) {
	res, err := c.doRequest(ctx, http.MethodGet, apiFolders, nil)

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response FolderResponse

	if err := res.Decode(&response); err != nil {
		return nil, err
	}

	// Root folder is response.Folder
	return response.Folder.Flatten(), nil
}

// GetFolder returns a single folder
func (c *client) GetFolder(ctx context.Context, folder string) (*Folder, error) {
	res, err := c.doRequest(ctx, http.MethodPost, apiFolder, folderRequest{
		Folder: folder,
	})

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var folderResponse FolderResponse

	if err := res.Decode(&folderResponse); err != nil {
		return nil, err
	}

	if !folderResponse.Success {
		if folderResponse.Message == "Folder not found" {
			return nil, ErrNoFolder
		}

		return nil, fmt.Errorf("received error from API: %s", folderResponse.Message)
	}

	return &folderResponse.Folder, nil
}

// filesRequest is a struct containing the appropriate fields for making a `GetFiles` request
type filesRequest struct {
	FileIDs []string `json:"fileIds"`
}

// GetFiles returns file data of the specified files
func (c *client) GetFiles(ctx context.Context, ids ...string) ([]File, error) {
	res, err := c.doRequest(ctx, http.MethodPost, apiFiles, filesRequest{
		FileIDs: ids,
	})

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response ListResponse

	if err := res.Decode(&response); err != nil {
		return nil, err
	}

	return response.Files, nil
}

// DeleteFiles deletes the remote files specified by ids
func (c *client) DeleteFiles(ctx context.Context, ids ...string) error {
	res, err := c.doRequest(ctx, http.MethodPost, apiDeleteFiles, filesRequest{
		FileIDs: ids,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response defaultResponse

	if err := res.Decode(&response); err != nil {
		return err
	}

	return nil
}

// DownloadFile opens the specified file as an io.ReadCloser, with optional `opts` (range header, etc)
func (c *client) DownloadFile(ctx context.Context, id string, opts ...RequestOpt) (io.ReadCloser, error) {
	res, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf(apiFileDownload, id), nil, opts...)

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	return res.Body, nil
}

// GetFileID gets a file id from a specified directory and file name
func (c *client) GetFileID(ctx context.Context, dir, fileName string) (string, error) {
	var folder *Folder

	if dir == "" || dir == "/" {
		folders, err := c.GetFolders(ctx)

		if err != nil {
			return "", err
		}

		folder = &folders[0]
	} else {
		var err error

		folder, err = c.GetFolder(ctx, dir)

		if err != nil {
			return "", err
		}
	}

	for _, file := range folder.Files {
		if file.Name == fileName {
			return file.ID, nil
		}
	}

	return "", ErrNoFile
}

// Find uses similar methods to GetFileID, but instead checks for both files AND folders
func (c *client) Find(ctx context.Context, file string) (*Folder, *File, error) {
	base, name := c.ParsePath(file)

	var folder *Folder

	if base == "" || base == "/" {
		folders, err := c.GetFolders(ctx)

		if err != nil {
			return nil, nil, err
		}

		folder = &folders[0]

		if name == "" {
			return folder, nil, nil
		}
	} else {
		var err error

		folder, err = c.GetFolder(ctx, base)

		if err != nil {
			return nil, nil, err
		}
	}

	for _, file := range folder.Files {
		if file.Name == name {
			return nil, &file, nil
		}
	}

	for _, folder := range folder.Subfolders {
		if folder.Name == name {
			return &folder, nil, nil
		}
	}

	return nil, nil, ErrNoFile
}

// folderRequest is used for creating and deleting folders
type folderRequest struct {
	ParentFolder string `json:"parentFolder,omitempty"`
	Folder       string `json:"folder"`
}

// CreateFolder creates a new remote folder
func (c *client) CreateFolder(ctx context.Context, folder string) (*Folder, error) {
	parent, subfolder := c.ParsePath(folder)

	res, err := c.doRequest(ctx, http.MethodPost, apiPutFolder, folderRequest{
		ParentFolder: parent,
		Folder:       subfolder,
	})

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response FolderResponse

	if err := res.Decode(&response); err != nil {
		return nil, err
	}

	if !response.Success {
		return nil, fmt.Errorf("failed to create directory, status: %d, response: %s", res.StatusCode, response.Message)
	}

	return &response.Folder, nil
}

// DeleteFolder deletes a specified folder by name
func (c *client) DeleteFolder(ctx context.Context, folder string) error {
	parent, subfolder := c.ParsePath(folder)

	res, err := c.doRequest(ctx, http.MethodPost, apiDeleteFolder, folderRequest{
		ParentFolder: parent,
		Folder:       subfolder,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var status defaultResponse

	if err := res.Decode(&status); err != nil {
		return err
	}

	if !status.Success {
		return fmt.Errorf("failed to remove directory, status: %d, response: %s", res.StatusCode, status.Message)
	}

	return nil
}

type moveFilesRequest struct {
	NewFolder string   `json:"newFolder"`
	FileIDs   []string `json:"fileIDs"`
}

// MoveFiles moves files to the specified folder
func (c *client) MoveFiles(ctx context.Context, folder string, fileIDs ...string) error {
	res, err := c.doRequest(ctx, http.MethodPost, apiMoveFiles, moveFilesRequest{
		NewFolder: folder,
		FileIDs:   fileIDs,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response FolderResponse

	if err := res.Decode(&response); err != nil {
		return err
	}

	if !response.Success {
		return fmt.Errorf("failed to create directory, status: %d, response: %s", res.StatusCode, response.Message)
	}

	return nil
}

type editFileRequest struct {
	NewFilename string `json:"newFilename"`
}

// RenameFile will rename the specified file to the new name
func (c *client) RenameFile(ctx context.Context, fileID string, name string) error {
	res, err := c.doRequest(ctx, http.MethodPost, apiEditFile, editFileRequest{
		NewFilename: name,
	}, WithURLParameter("fileId", fileID))

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response defaultResponse

	if err := res.Decode(&response); err != nil {
		return err
	}

	if !response.Success {
		return fmt.Errorf("failed to create directory, status: %d, response: %s", res.StatusCode, response.Message)
	}

	return nil
}

type EditFileParams struct {
	Password           string    `json:"password"`
	Published          bool      `json:"published"`
	PublishedUntil     time.Time `json:"publishedUntil"`
	ShortLink          string    `json:"shortLink"`
	PublicDownloadLink string    `json:"publicDownloadLink"`
}

// EditFile updates a file on the backend
func (c *client) EditFile(ctx context.Context, fileID string, params EditFileParams) error {
	res, err := c.doRequest(ctx, http.MethodPost, apiEditFile, params, WithURLParameter("fileId", fileID))

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response defaultResponse

	if err := res.Decode(&response); err != nil {
		return err
	}

	if !response.Success {
		return fmt.Errorf("failed to create directory, status: %d, response: %s", res.StatusCode, response.Message)
	}

	return nil
}

type linkResponse struct {
	defaultResponse
	PublicLink string `json:"publicLink"`
	ShortLink  string `json:"shortLink"`
	IsPublic   bool   `json:"isPublic"`
}

// GetLink creates a short link and public link to a file
// This is combined with EditFile to make it public
func (c *client) GetLink(ctx context.Context, fileID string) (string, string, error) {
	res, err := c.doRequest(ctx, http.MethodGet, apiGetFileLink, nil, WithURLParameter("fileId", fileID))

	if err != nil {
		return "", "", err
	}

	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("%w: %d", ErrUnexpectedStatus, res.StatusCode)
	}

	var response linkResponse

	if err := res.Decode(&response); err != nil {
		return "", "", err
	}

	if !response.Success {
		return "", "", fmt.Errorf("failed to create directory, status: %d, response: %s", res.StatusCode, response.Message)
	}

	return response.ShortLink, response.PublicLink, nil
}

type patchFolderRequest struct {
	folderRequest
	ParentFolder    string `json:"parentFolder"`
	Folder          string `json:"folder"`
	NewFolderName   string `json:"newFolderName,omitempty"`
	NewParentFolder string `json:"newParentFolder,omitempty"`
}

func (c *client) MoveFolder(ctx context.Context, folder, newParentFolder string) error {
	_, subfolder := c.ParsePath(folder)

	res, err := c.doRequest(ctx, http.MethodPost, apiPatchFolder, patchFolderRequest{
		//ParentFolder:    parent,
		Folder:          folder,
		NewParentFolder: newParentFolder,
		NewFolderName:   subfolder,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d (%s)", ErrUnexpectedStatus, res.StatusCode, string(res.Data()))
	}

	var response defaultResponse

	if err := res.Decode(&response); err != nil {
		return err
	}

	if !response.Success {
		return fmt.Errorf("failed to move directory, status: %d, response: %s", res.StatusCode, response.Message)
	}

	return nil
}

// File represents a file object on the remote server, identified by `ID`
type File struct {
	ID         string    `json:"id"`
	Name       string    `json:"fileName"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	DateAdded  time.Time `json:"dateAdded"`
	FolderPath string    `json:"folderPath"`
}

// Folder represents a folder object on the remote server
type Folder struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Size       int64    `json:"size"`
	Version    string   `json:"version"`
	Count      int      `json:"count"`
	Subfolders []Folder `json:"subfolders"`
	Files      []File   `json:"files"`
}

// Flatten takes all folders and subfolders, returning them as a single slice
func (f Folder) Flatten() []Folder {
	folders := []Folder{f}

	for _, folder := range f.Subfolders {
		folders = append(folders, folder.Flatten()...)
	}

	return folders
}

func (f Folder) Subfolder(name string) *Folder {
	for _, folder := range f.Subfolders {
		if folder.Name == name {
			return &folder
		}
	}

	return nil
}
