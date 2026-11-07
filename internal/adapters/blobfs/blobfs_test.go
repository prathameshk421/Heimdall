package blobfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestPutAndGetByContentHash(t *testing.T) {
	store, err := New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	content := []byte("durable blob")
	ref, err := store.Put(context.Background(), ports.BlobEvidence, bytes.NewReader(content))
	require.NoError(t, err)
	want := "blob:sha256:" + func() string { sum := sha256.Sum256(content); return hex.EncodeToString(sum[:]) }()
	require.Equal(t, want, string(ref))

	reader, err := store.Get(context.Background(), ref)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, content, got)
}

func TestPutUsesClassDirectoryAndRejectsUnknownClass(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ref, err := store.Put(context.Background(), ports.BlobLog, strings.NewReader("log"))
	require.NoError(t, err)
	require.FileExists(t, root+"/log/sha256/"+string(ref)[len("blob:sha256:"):len("blob:sha256:")+2]+"/"+string(ref)[len("blob:sha256:")+2:len("blob:sha256:")+4]+"/"+string(ref)[len("blob:sha256:"):])
	_, err = store.Put(context.Background(), ports.BlobClass("unknown"), strings.NewReader("x"))
	require.Error(t, err)
}

func TestGetRejectsTraversalAndMissingBlob(t *testing.T) {
	store, err := New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = store.Get(context.Background(), workflow.BlobRef("blob:sha256:../../etc/passwd"))
	require.Error(t, err)
	_, err = store.Get(context.Background(), workflow.BlobRef("blob:sha256:"+strings.Repeat("0", 64)))
	require.ErrorIs(t, err, ports.ErrNotFound)
}

func TestConcurrentPutSameContentIsIdempotent(t *testing.T) {
	store, err := New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	const workers = 16
	refs := make([]string, workers)
	var wg sync.WaitGroup
	for i := range refs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref, putErr := store.Put(context.Background(), ports.BlobBundle, strings.NewReader("same"))
			if putErr != nil {
				t.Errorf("put: %v", putErr)
				return
			}
			refs[i] = string(ref)
		}(i)
	}
	wg.Wait()
	for _, ref := range refs {
		require.Equal(t, refs[0], ref)
	}
}
