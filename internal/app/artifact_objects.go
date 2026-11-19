package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/store"
)

const DefaultArtifactCacheLimit int64 = 1 << 30

func (a App) artifactRoot() string {
	return filepath.Join(filepath.Dir(store.Path(a.DataDir)), "artifacts")
}

func artifactLocation(hash string) string { return "objects/sha256/" + hash }

func validArtifactHash(hash string) bool {
	if len(hash) != sha256.Size*2 || strings.ToLower(hash) != hash {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

// Check before opening so directories, symlinks and special files are never read
// as payloads. The opened handle must still refer to the same regular file.
func openArtifactFile(path string) (*os.File, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, Fail("invalid-path", "artifact payload must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, 0, Fail("artifact-changed", "artifact payload changed while opening it")
	}
	return f, opened.Size(), nil
}

func readArtifactSource(path string, limit int64) (ArtifactPayload, *os.File, error) {
	f, size, err := openArtifactFile(path)
	if err != nil {
		return ArtifactPayload{}, nil, err
	}
	if size > limit {
		f.Close()
		return ArtifactPayload{}, nil, Fail("artifact-cache-full", "payload exceeds the local artifact cache limit")
	}
	hash, err := hashArtifact(f, io.Discard, size)
	if err != nil {
		f.Close()
		return ArtifactPayload{}, nil, err
	}
	return ArtifactPayload{
		Name: filepath.Base(path), Size: size, SHA256: hash,
		Storage: "local-only", Location: artifactLocation(hash),
	}, f, nil
}

// Bound all reads by the expected size, including reads of a file that grows
// while it is being registered. The extra byte is never copied to the cache.
func hashArtifact(src io.Reader, dst io.Writer, size int64) (string, error) {
	hash := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(dst, hash), src, size); err != nil {
		if errors.Is(err, io.EOF) {
			return "", Fail("artifact-hash-mismatch", "artifact payload size does not match")
		}
		return "", err
	}
	var extra [1]byte
	if _, err := io.ReadFull(src, extra[:]); !errors.Is(err, io.EOF) {
		if err != nil {
			return "", err
		}
		return "", Fail("artifact-hash-mismatch", "artifact payload size does not match")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyArtifact(src io.Reader, dst io.Writer, p ArtifactPayload) error {
	hash, err := hashArtifact(src, dst, p.Size)
	if err == nil && hash != p.SHA256 {
		return Fail("artifact-hash-mismatch", "artifact payload SHA-256 does not match")
	}
	return err
}

func (a App) cacheArtifact(ctx context.Context, c *sql.Conn, src *os.File, p ArtifactPayload, limit int64) error {
	path := filepath.Join(a.artifactRoot(), filepath.FromSlash(p.Location))
	existing, size, err := openArtifactFile(path)
	if err == nil {
		defer existing.Close()
		if size != p.Size {
			return Fail("artifact-hash-mismatch", "cached artifact payload size does not match")
		}
		return verifyArtifact(existing, io.Discard, p)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	staging := filepath.Join(dir, ".incoming")
	if err := os.Remove(staging); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := makeArtifactRoom(ctx, c, dir, limit-p.Size); err != nil {
		return err
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	out, err := os.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(staging)
	if err := finishArtifactCopy(src, out, p); err != nil {
		return err
	}
	return os.Rename(staging, path)
}

// The catalog write lock makes unreferenced objects safe to reclaim after a
// crash between object publication and manifest commit. Only inspect the
// managed, flat object directory.
func makeArtifactRoom(ctx context.Context, c *sql.Conn, dir string, remaining int64) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		hash := entry.Name()
		if !validArtifactHash(hash) {
			continue
		}
		var referenced bool
		if err := c.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM artifacts WHERE sha256=?)", hash).Scan(&referenced); err != nil {
			return err
		}
		if !referenced {
			if err := os.Remove(filepath.Join(dir, hash)); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		remaining -= info.Size()
		if remaining < 0 {
			return Fail("artifact-cache-full", "local artifact cache is full; increase CTXFERRY_ARTIFACT_CACHE_BYTES")
		}
	}
	return nil
}

func finishArtifactCopy(src io.Reader, out *os.File, p ArtifactPayload) error {
	defer out.Close()
	if err := verifyArtifact(src, out, p); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

func (a App) fetchArtifactObject(p ArtifactPayload, output string) error {
	if !validArtifactHash(p.SHA256) || p.Size < 0 || p.Location != artifactLocation(p.SHA256) || p.Storage != "local-only" {
		return Fail("artifact-invalid", "artifact manifest has an invalid payload location or identity")
	}
	source := filepath.Join(a.artifactRoot(), filepath.FromSlash(p.Location))
	in, size, err := openArtifactFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return Fail("artifact-unavailable", "artifact payload is missing from the local cache")
	}
	if err != nil {
		return err
	}
	defer in.Close()
	if size != p.Size {
		return Fail("artifact-hash-mismatch", "cached artifact payload size does not match")
	}
	dir, err := canonical(filepath.Dir(output))
	if err != nil {
		return err
	}
	root, err := canonical(a.DataDir)
	if err != nil {
		return err
	}
	if contains(root, dir) {
		return Fail("invalid-path", "artifact output must be outside the application data directory")
	}
	out, err := os.CreateTemp(dir, ".ctxferry-fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if err := finishArtifactCopy(in, out, p); err != nil {
		return err
	}
	// Publish the fully verified file without overwriting an existing path.
	if err := os.Link(out.Name(), filepath.Join(dir, filepath.Base(output))); errors.Is(err, os.ErrExist) {
		return Fail("output-exists", "artifact output file already exists")
	} else {
		return err
	}
}
