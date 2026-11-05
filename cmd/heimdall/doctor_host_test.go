package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostResourceChecksEnforceDocumentedMinimums(t *testing.T) {
	command := func(_ context.Context, name string, _ ...string) (string, error) {
		switch name {
		case "free":
			return "Mem: 8192 2048 1024 0 5120 5000\nSwap: 0 0 0\n", nil
		case "df":
			return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 100000000 1 40000000 1% /\n", nil
		case "nproc":
			return "1\n", nil
		default:
			return "", errors.New("unexpected command")
		}
	}
	require.Equal(t, statusFail, resourceCheck(context.Background(), command, "free", []string{"-m"}, "memory").Status)
	require.Equal(t, statusFail, resourceCheck(context.Background(), command, "df", []string{"-Pk", "/"}, "disk").Status)
	require.Equal(t, statusFail, cpuCheck(context.Background(), command).Status)
}

func TestHostResourceChecksAcceptDocumentedMinimums(t *testing.T) {
	command := func(_ context.Context, name string, _ ...string) (string, error) {
		switch name {
		case "free":
			return "Mem: 16384 2048 1024 0 13312 12000\nSwap: 4096 0 4096\n", nil
		case "df":
			return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 100000000 1 60000000 1% /\n", nil
		case "nproc":
			return "2\n", nil
		default:
			return "", errors.New("unexpected command")
		}
	}
	require.Equal(t, statusOK, resourceCheck(context.Background(), command, "free", []string{"-m"}, "memory").Status)
	disk := resourceCheck(context.Background(), command, "df", []string{"-Pk", "/"}, "disk")
	require.Equal(t, statusOK, disk.Status, disk.Detail)
	require.Equal(t, statusOK, cpuCheck(context.Background(), command).Status)
}

func TestMetadataOptionsCheckRequiresIMDSv2AndHopLimitOne(t *testing.T) {
	command := func(context.Context, string, ...string) (string, error) {
		return `{"HttpTokens":"optional","HttpPutResponseHopLimit":2}`, nil
	}
	result := metadataOptionsCheck(context.Background(), command)
	require.Equal(t, statusFail, result.Status)
	require.Contains(t, result.Detail, "IMDSv2")
	command = func(context.Context, string, ...string) (string, error) {
		return `{"HttpTokens":"required","HttpPutResponseHopLimit":1}`, nil
	}
	require.Equal(t, statusOK, metadataOptionsCheck(context.Background(), command).Status)
}
