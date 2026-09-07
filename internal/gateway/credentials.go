package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Serialize token creation across the desktop and concurrent connect processes.
func (m *Manager) lockCredentials(ctx context.Context) (func(), error) {
	dir := filepath.Join(m.Dir, "credentials")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func readLocalToken(path string) (string, error) {
	if err := os.Chmod(path, 0600); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := string(b)
	if value == "" || strings.ContainsAny(value, " \r\n\t") {
		return "", errors.New("本地 token 文件内容无效")
	}
	return value, nil
}

func (m *Manager) Key(name string) (string, error) {
	if !serviceNamePattern.MatchString(name) {
		return "", errors.New("凭证标识无效")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	unlock, err := m.lockCredentials(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	path := filepath.Join(m.Dir, "credentials", name)
	value, err := readLocalToken(path)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读取本地 token: %w", err)
	}
	value = rand.Text() + rand.Text()
	if err := AtomicWrite(path, []byte(value)); err != nil {
		return "", fmt.Errorf("保存本地 token: %w", err)
	}
	return value, nil
}
