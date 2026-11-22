package blobfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// DefaultRetention returns the retention window per time-expiring blob class (TECH_SPEC 4.3, P5).
// Evidence, pack, and knowledge blobs are retained by reference/revocation, not age.
func DefaultRetention() map[ports.BlobClass]time.Duration {
	const ninetyDays = 90 * 24 * time.Hour
	return map[ports.BlobClass]time.Duration{
		ports.BlobLog:    ninetyDays,
		ports.BlobDiff:   ninetyDays,
		ports.BlobBundle: ninetyDays,
	}
}

// SweepExpired removes blobs older than each class's retention window as of now and returns how many were deleted (P5).
func (s *Store) SweepExpired(ctx context.Context, now time.Time, retention map[ports.BlobClass]time.Duration) (int, error) {
	if len(retention) == 0 {
		retention = DefaultRetention()
	}
	deleted := 0
	for class, ttl := range retention {
		if ttl <= 0 || !class.Valid() {
			continue
		}
		cutoff := now.Add(-ttl)
		n, err := s.sweepClass(ctx, class, cutoff)
		if err != nil {
			return deleted, err
		}
		deleted += n
	}
	return deleted, nil
}

func (s *Store) sweepClass(ctx context.Context, class ports.BlobClass, cutoff time.Time) (int, error) {
	rootFS := s.root.FS()
	classDir := string(class)
	if _, err := fs.Stat(rootFS, classDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil // class directory has no blobs yet
		}
		return 0, fmt.Errorf("stat %s blobs: %w", class, err)
	}
	deleted := 0
	err := fs.WalkDir(rootFS, classDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(path.Base(p), ".tmp-") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := s.root.Remove(p); err != nil {
				return fmt.Errorf("remove expired blob %s: %w", p, err)
			}
			deleted++
		}
		return nil
	})
	if err != nil {
		return deleted, fmt.Errorf("sweep %s blobs: %w", class, err)
	}
	return deleted, nil
}
