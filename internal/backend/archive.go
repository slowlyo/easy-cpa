package backend

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// extractCoreAsset 解压核心资产并保留原始可执行文件名。
func extractCoreAsset(archivePath, targetDir string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZipFile(archivePath, targetDir)
	}
	if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
		return extractTarGzFile(archivePath, targetDir)
	}
	return fmt.Errorf("不支持的压缩格式: %s", archivePath)
}

// extractZipFile 解压 zip 文件。
func extractZipFile(path, targetDir string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if err := writeZipEntry(file, targetDir); err != nil {
			return err
		}
	}
	return nil
}

// writeZipEntry 写入单个 zip 条目。
func writeZipEntry(file *zip.File, targetDir string) error {
	name := filepath.Base(file.Name)
	target := filepath.Join(targetDir, name)
	if file.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("打开 zip 条目失败: %w", err)
	}
	defer src.Close()
	return writeFileFromReader(target, src, file.Mode())
}

// extractTarGzFile 解压 tar.gz 文件。
func extractTarGzFile(path, targetDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开 tar.gz 失败: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("打开 gzip 流失败: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 tar 条目失败: %w", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		target := filepath.Join(targetDir, filepath.Base(header.Name))
		if err := writeFileFromReader(target, tarReader, os.FileMode(header.Mode)); err != nil {
			return err
		}
	}
}

// writeFileFromReader 将 Reader 内容写入目标文件。
func writeFileFromReader(target string, src io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}
	dst, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("创建解压文件失败: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("写入解压文件失败: %w", err)
	}
	if runtime.GOOS != "windows" {
		if mode == 0 {
			mode = 0o755
		}
		if err := os.Chmod(target, mode); err != nil {
			return fmt.Errorf("设置执行权限失败: %w", err)
		}
	}
	return nil
}
