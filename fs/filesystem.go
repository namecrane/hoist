package fs

import (
	"context"
	"errors"
	"github.com/namecrane/hoist"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"gopkg.in/djherbis/fscache.v0"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
)

var ErrNotSupported = errors.New("not supported")

var _ afero.Fs = (*FileSystem)(nil)

type Option func(f *FileSystem)

// WithWriteFs overwrites the in-memory temporary filesystem used for writes
func WithWriteFs(tempFs afero.Fs) Option {
	return func(f *FileSystem) {
		f.tempFs = tempFs
	}
}

// WithReadCache defines a fscache.Cache instance which allows use of `ReaderAt` functionality
func WithReadCache(cache fscache.Cache) Option {
	return func(f *FileSystem) {
		f.readCache = cache
	}
}

func New(c hoist.Client, opts ...Option) *FileSystem {
	f := &FileSystem{
		client: c,
	}

	for _, opt := range opts {
		opt(f)
	}

	if f.tempFs == nil {
		f.tempFs = afero.NewMemMapFs()
	}

	return f
}

type FileSystem struct {
	client hoist.Client

	// Used for writing files, can be any afero.Fs
	tempFs afero.Fs

	// Used for reading files when they request "ReadAt"
	readCache fscache.Cache
}

// Create will create a new file (an empty CraneFile)
func (c *FileSystem) Create(name string) (afero.File, error) {
	path, sub := c.client.ParsePath(name)

	return &CraneFile{
		fs:     c,
		path:   path,
		name:   sub,
		tempFs: c.tempFs,
	}, nil
}

func (c *FileSystem) Open(name string) (afero.File, error) {
	return c.OpenFile(name, os.O_RDONLY, 0)
}

func (c *FileSystem) Remove(name string) error {
	folder, file, err := c.client.Find(context.Background(), name)

	if err != nil {
		return err
	}

	log.WithField("name", name).Debug("Removing file")

	if folder != nil {
		return c.client.DeleteFolder(context.Background(), folder.Path)
	} else if file != nil {
		log.WithField("id", file.ID).Debug("Removing file id")
		return c.client.DeleteFiles(context.Background(), file.ID)
	}

	return nil
}

func (c *FileSystem) Name() string {
	return "NameCrane Hoist"
}

func (c *FileSystem) Chmod(name string, mode os.FileMode) error {
	return ErrNotSupported
}

func (c *FileSystem) Chown(name string, uid, gid int) error {
	return ErrNotSupported
}

func (c *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return ErrNotSupported
}

func (c *FileSystem) Mkdir(name string, perm os.FileMode) error {
	ctx := context.Background()

	log.WithField("name", name).Debug("Mkdir")

	parent, sub := c.client.ParsePath(name)

	parentFolder, _, err := c.client.Find(ctx, parent)

	if err != nil {
		log.WithError(err).Warning("Failed to call find")
		return err
	}

	log.WithField("parent", parent).WithField("sub", sub).Debug("Create folders")

	subfolder := parentFolder.Subfolder(sub)

	if subfolder != nil {
		log.WithField("folder", name).Debug("Folder already exists")
		return nil
	}

	log.WithField("folder", path.Join(parentFolder.Path, sub)).Debug("Creating folder")

	subfolder, err = c.client.CreateFolder(ctx, path.Join(parentFolder.Path, sub))

	if err != nil {
		return err
	}

	log.WithField("folder", name).Debug("Created folder")

	return nil
}

func (c *FileSystem) MkdirAll(path string, perm os.FileMode) error {
	ctx := context.Background()
	log.WithField("name", path).Debug("MkdirAll")

	folder, _, err := c.client.Find(ctx, path)

	if errors.Is(err, hoist.ErrNoFile) {
		// OK
	} else if err != nil {
		log.WithError(err).Warning("Failed to call find")
		return err
	} else if folder != nil {
		return nil
	}

	folders, err := c.client.GetFolders(ctx)

	if err != nil {
		log.WithError(err).Warning("Failed to get root folder")
		return err
	}

	parts := strings.Split(path, "/")

	log.WithField("parts", parts).Debug("Create folders")

	if err := c.recursiveMkdir(ctx, parts, folders[0]); err != nil {
		return err
	}

	log.WithField("folder", path).Debug("Created folder")

	return nil
}

func (c *FileSystem) recursiveMkdir(ctx context.Context, parts []string, currentFolder hoist.Folder) error {
	log.Info("Create directory", currentFolder.Name, parts)
	subfolder := currentFolder.Subfolder(parts[0])

	if subfolder == nil {
		var err error
		subfolder, err = c.client.CreateFolder(ctx, path.Join(currentFolder.Path, parts[0]))

		if err != nil {
			return err
		}
	}

	if len(parts) > 1 {
		return c.recursiveMkdir(ctx, parts[1:], *subfolder)
	}

	return nil
}

func (c *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	folder, file, err := c.client.Find(context.Background(), name)

	if err != nil && !errors.Is(err, hoist.ErrNoFile) {
		return nil, err
	}

	fields := log.Fields{
		"name": name,
	}

	if folder != nil {
		log.WithFields(fields).Debug("Opening folder")
	} else if file != nil {
		log.WithFields(fields).Debug("Opening file")
	}

	p, sub := c.client.ParsePath(name)

	return &CraneFile{
		mode:   flag,
		fs:     c,
		path:   p,
		name:   sub,
		file:   file,
		folder: folder,
		tempFs: c.tempFs,
	}, nil
}

func (c *FileSystem) RemoveAll(name string) error {
	return c.Remove(name)
}

func (c *FileSystem) Rename(oldName, newName string) error {
	folder, file, err := c.client.Find(context.Background(), oldName)

	if err != nil {
		return err
	}

	oldBase, _ := c.client.ParsePath(oldName)
	base, name := c.client.ParsePath(newName)

	if folder != nil {
		var newParent string

		if base != oldBase {
			newParent = base
		}

		return c.client.MoveFolder(context.Background(), folder.Path, newParent, name)
	} else if file != nil {
		return c.client.RenameFile(context.Background(), file.ID, path.Base(newName))
	}

	return nil
}

func (c *FileSystem) Stat(name string) (os.FileInfo, error) {
	folder, file, err := c.client.Find(context.Background(), name)

	if errors.Is(err, hoist.ErrNoFile) {
		return nil, fs.ErrNotExist
	} else if err != nil {
		return nil, err
	}

	return &CraneFileInfo{folder: folder, file: file}, nil
}
