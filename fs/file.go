package fs

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/namecrane/hoist"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"gopkg.in/djherbis/fscache.v0"
	"io"
	"io/fs"
	"os"
	"path"
	"time"
)

var ErrEmptyFile = errors.New("file is empty")

type ReaderAtSeeker interface {
	io.ReaderAt
	io.Seeker
}

type CraneFile struct {
	fs            *FileSystem
	mode          int
	path          string
	name          string
	file          *hoist.File
	folder        *hoist.Folder
	tempFs        afero.Fs
	temporaryFile afero.File
	readStream    io.ReadCloser
	readAtStream  fscache.ReadAtCloser
}

func (c *CraneFile) Open(mode int) error {
	// When creating a new file, explicitly open a temp file
	if mode&os.O_CREATE != 0 {
		return c.openTempFile()
	}
	return nil
}

func (c *CraneFile) ID() string {
	if c.file != nil {
		return c.file.ID
	}

	return ""
}

func (c *CraneFile) ReadAt(p []byte, off int64) (n int, err error) {
	if c.fs.readCache == nil {
		return -1, ErrNotSupported
	}

	log.WithFields(log.Fields{
		"file":   c.path + "/" + c.name,
		"size":   len(p),
		"offset": off,
	}).Debug("Reading file bytes")

	if c.readAtStream == nil {
		log.WithField("path", c.path).Debug("Opening cache read file")
		if err := c.openReadAtStream(); err != nil {
			return -1, err
		}
	}

	if off >= c.file.Size {
		return 0, io.EOF
	}

	return c.readAtStream.ReadAt(p, off)
}

func (c *CraneFile) WriteAt(p []byte, off int64) (n int, err error) {
	if c.temporaryFile == nil {
		// Create file to write to
		if err := c.openTempFile(); err != nil {
			return -1, err
		}
	}

	return c.temporaryFile.WriteAt(p, off)
}

func (c *CraneFile) Name() string {
	if c.file != nil {
		return c.file.Name
	}

	if c.folder != nil {
		return c.folder.Name
	}

	return ""
}

func (c *CraneFile) Readdirnames(n int) ([]string, error) {
	if c.folder == nil {
		return nil, fs.ErrNotExist
	}

	names := make([]string, 0)

	for _, dir := range c.folder.Subfolders {
		names = append(names, dir.Name)
	}

	return names, nil
}

func (c *CraneFile) Sync() error {
	return nil
}

func (c *CraneFile) Truncate(size int64) error {
	//TODO implement me
	panic("implement me")
}

func (c *CraneFile) WriteString(s string) (ret int, err error) {
	return c.Write([]byte(s))
}

func (c *CraneFile) Close() error {
	if c.temporaryFile != nil {
		return c.uploadFile()
	} else if c.readStream != nil {
		return c.readStream.Close()
	} else if c.readAtStream != nil {
		return c.readAtStream.Close()
	}

	return nil
}

func (c *CraneFile) uploadFile() error {
	var err error

	if err = c.temporaryFile.Close(); err != nil {
		return err
	}

	var f afero.File

	defer func() {
		if f != nil {
			_ = f.Close()
		}

		// Clean up the file when we're done
		_ = c.tempFs.Remove(c.temporaryFile.Name())
	}()

	f, err = c.tempFs.Open(c.temporaryFile.Name())

	if err != nil {
		return err
	}

	stat, err := f.Stat()

	if err != nil {
		return err
	}

	if stat.Size() == 0 {
		return ErrEmptyFile
	}

	file, err := c.fs.client.ChunkedUpload(context.Background(), f, path.Join(c.path, c.name), stat.Size())

	if err != nil {
		return err
	}

	c.file = file

	return err
}

func (c *CraneFile) Read(p []byte) (n int, err error) {
	// If something tries to read from a file that does not exist, make sure we catch it
	if c.file == nil {
		return -1, io.ErrUnexpectedEOF
	}

	// Support cached reads
	if c.readAtStream != nil {
		return c.readAtStream.Read(p)
	}

	// Open direct read stream
	if c.readStream == nil {
		if err := c.openReadStream(); err != nil {
			return -1, err
		}
	}

	return c.readStream.Read(p)
}

func (c *CraneFile) openReadStream() error {
	stream, err := c.fs.client.DownloadFile(context.Background(), c.file.ID)

	if err != nil {
		return err
	}

	c.readStream = stream

	return nil
}

func (c *CraneFile) openReadAtStream() error {
	if c.fs.readCache == nil {
		return ErrNotSupported
	}

	read, write, err := c.fs.readCache.Get(c.ID())

	if err != nil {
		return err
	}

	if write != nil {
		// Open read stream
		if err := c.openReadStream(); err != nil {
			return err
		}

		defer func() {
			defer c.readStream.Close()

			n, err := io.Copy(write, c.readStream)

			if err != nil {
				log.WithError(err).Warning("Failed to copy to cache")
				return
			}

			log.WithFields(log.Fields{
				"file":   c.path + "/" + c.name,
				"copied": n,
				"size":   c.file.Size,
			}).Debug("Copied file to cache")
		}()
	}

	c.readAtStream = read

	return nil
}

func (c *CraneFile) Seek(offset int64, whence int) (int64, error) {
	log.WithFields(log.Fields{
		"file":   c.path + "/" + c.name,
		"whence": whence,
		"offset": offset,
	}).Debug("Seek")

	return -1, ErrNotSupported
}

func (c *CraneFile) Readdir(count int) ([]fs.FileInfo, error) {
	if c.folder == nil {
		return nil, fs.ErrNotExist
	}

	var info []fs.FileInfo

	for _, folder := range c.folder.Subfolders {
		info = append(info, &CraneFileInfo{folder: &folder})
	}

	for _, file := range c.folder.Files {
		info = append(info, &CraneFileInfo{file: &file})
	}

	return info, nil
}

func (c *CraneFile) Stat() (fs.FileInfo, error) {
	if c.file != nil || c.folder != nil {
		return &CraneFileInfo{file: c.file, folder: c.folder}, nil
	}

	return nil, fs.ErrNotExist
}

func (c *CraneFile) openTempFile() error {
	u, err := uuid.NewV7()

	if err != nil {
		return err
	}

	tempFile, err := c.tempFs.Create(u.String())

	if err != nil {
		return err
	}

	c.temporaryFile = tempFile
	return nil
}

func (c *CraneFile) Write(p []byte) (n int, err error) {
	if c.temporaryFile == nil {
		// Create file to write to
		if err := c.openTempFile(); err != nil {
			return -1, err
		}
	}

	return c.temporaryFile.Write(p)
}

// NewFileInfo creates a new CraneFileInfo struct, used for reading directories/etc
func NewFileInfo(file *hoist.File, folder *hoist.Folder) *CraneFileInfo {
	return &CraneFileInfo{file: file, folder: folder}
}

type CraneFileInfo struct {
	file   *hoist.File
	folder *hoist.Folder
}

func (c *CraneFileInfo) Name() string {
	if c.file != nil {
		return c.file.Name
	}

	if c.folder != nil {
		return c.folder.Name
	}

	return "<unknown>"
}

func (c *CraneFileInfo) Size() int64 {
	if c.file != nil {
		return c.file.Size
	}

	return -1
}

func (c *CraneFileInfo) Mode() fs.FileMode {
	if c.folder != nil {
		return 0755 | fs.ModeDir
	}

	return fs.FileMode(0644)
}

func (c *CraneFileInfo) ModTime() time.Time {
	if c.file != nil {
		return c.file.DateAdded
	}

	return time.Now()
}

func (c *CraneFileInfo) IsDir() bool {
	return c.folder != nil
}

func (c *CraneFileInfo) Sys() any {
	return nil
}
