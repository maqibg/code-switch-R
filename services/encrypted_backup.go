package services

import (
	"archive/tar"
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeswitch/internal/infra"
	"golang.org/x/crypto/argon2"
)

const (
	encryptedBackupExt       = ".csrbackup"
	encryptedBackupVersion   = 1
	encryptedBackupChunkSize = 1 << 20
	encryptedBackupMaxHeader = 16 << 10
	encryptedBackupMaxFile   = 20 << 30
)

var encryptedBackupMagic = []byte("CSRBACKUP1\n")

type encryptedBackupHeader struct {
	Version      int    `json:"version"`
	CreatedAt    string `json:"created_at"`
	KDF          string `json:"kdf"`
	ArgonTime    uint32 `json:"argon_time"`
	ArgonMemory  uint32 `json:"argon_memory_kib"`
	ArgonThreads uint8  `json:"argon_threads"`
	Salt         string `json:"salt"`
	Cipher       string `json:"cipher"`
	NoncePrefix  string `json:"nonce_prefix"`
	ChunkSize    int    `json:"chunk_size"`
}

type EncryptedBackupResult struct {
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	Bytes     int64  `json:"bytes"`
	Warning   string `json:"warning,omitempty"`
}

func (is *ImportService) ExportEncryptedBackup(path, password string) (EncryptedBackupResult, error) {
	if err := validateBackupPassword(password); err != nil {
		return EncryptedBackupResult{}, err
	}
	target, err := resolveTransferOutputFile(path, encryptedBackupExt)
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	snapshotDir, err := os.MkdirTemp("", "code-switch-r-backup-")
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	defer os.RemoveAll(snapshotDir)

	snapshot, err := is.exportFullSnapshotDirectory(snapshotDir)
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	if snapshot.Warning != "" {
		return EncryptedBackupResult{}, fmt.Errorf("完整备份快照不完整: %s", snapshot.Warning)
	}
	if err := infra.AtomicWriteStream(target, 0o600, func(writer io.Writer) error {
		return encryptSnapshotDirectory(snapshotDir, password, writer)
	}); err != nil {
		return EncryptedBackupResult{}, fmt.Errorf("写入加密备份失败: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	return EncryptedBackupResult{Path: target, FileCount: snapshot.CopiedFileCount, Bytes: info.Size()}, nil
}

func (is *ImportService) RestoreEncryptedBackup(path, password string) (EncryptedBackupResult, error) {
	if err := validateBackupPassword(password); err != nil {
		return EncryptedBackupResult{}, err
	}
	source := expandTransferPath(path)
	if source == "" {
		return EncryptedBackupResult{}, fmt.Errorf("加密备份路径不能为空")
	}
	file, err := os.Open(source)
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil {
		return EncryptedBackupResult{}, statErr
	} else if !info.Mode().IsRegular() || info.Size() > encryptedBackupMaxFile {
		return EncryptedBackupResult{}, fmt.Errorf("备份文件类型或大小不受支持")
	}

	stagingDir, err := os.MkdirTemp("", "code-switch-r-restore-")
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	defer os.RemoveAll(stagingDir)
	fileCount, err := decryptBackupToDirectory(file, password, stagingDir)
	if err != nil {
		return EncryptedBackupResult{}, fmt.Errorf("解密备份失败，密码错误或文件已损坏: %w", err)
	}
	if !FileExists(filepath.Join(stagingDir, projectExportManifestFile)) || !FileExists(filepath.Join(stagingDir, appDatabaseFilename)) {
		return EncryptedBackupResult{}, fmt.Errorf("备份缺少完整性清单或主数据库")
	}

	rollbackDir, err := os.MkdirTemp("", "code-switch-r-rollback-")
	if err != nil {
		return EncryptedBackupResult{}, err
	}
	defer os.RemoveAll(rollbackDir)
	rollbackSnapshot, err := is.exportFullSnapshotDirectory(rollbackDir)
	if err != nil || rollbackSnapshot.Warning != "" {
		if err == nil {
			err = fmt.Errorf("%s", rollbackSnapshot.Warning)
		}
		return EncryptedBackupResult{}, fmt.Errorf("创建恢复前回滚快照失败: %w", err)
	}

	result, applyErr := is.applyFullSnapshotDirectory(stagingDir)
	if applyErr != nil || result.Warning != "" {
		rollbackResult, rollbackErr := is.applyFullSnapshotDirectory(rollbackDir)
		if rollbackErr != nil || rollbackResult.Warning != "" {
			return EncryptedBackupResult{}, fmt.Errorf("恢复失败且回滚失败: restore=%v warning=%s rollback=%v rollback_warning=%s", applyErr, result.Warning, rollbackErr, rollbackResult.Warning)
		}
		if applyErr == nil {
			applyErr = fmt.Errorf("%s", result.Warning)
		}
		return EncryptedBackupResult{}, fmt.Errorf("恢复失败，已回滚: %w", applyErr)
	}
	if is.appSettings != nil {
		is.appSettings.mu.Lock()
		is.appSettings.cacheValid = false
		is.appSettings.mu.Unlock()
	}
	info, _ := os.Stat(source)
	var size int64
	if info != nil {
		size = info.Size()
	}
	return EncryptedBackupResult{
		Path: source, FileCount: fileCount, Bytes: size,
		Warning: "完整备份已恢复；请重启应用以重新加载全部运行时状态。",
	}, nil
}

func validateBackupPassword(password string) error {
	length := len([]rune(password))
	if length < 8 {
		return fmt.Errorf("备份密码至少需要 8 个字符")
	}
	if len(password) > 4096 {
		return fmt.Errorf("备份密码过长")
	}
	return nil
}

func encryptSnapshotDirectory(snapshotDir, password string, output io.Writer) error {
	salt := make([]byte, 16)
	noncePrefix := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := rand.Read(noncePrefix); err != nil {
		return err
	}
	header := encryptedBackupHeader{
		Version: encryptedBackupVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		KDF: "argon2id", ArgonTime: 3, ArgonMemory: 64 * 1024, ArgonThreads: 2,
		Salt: base64.StdEncoding.EncodeToString(salt), Cipher: "aes-256-gcm",
		NoncePrefix: base64.StdEncoding.EncodeToString(noncePrefix), ChunkSize: encryptedBackupChunkSize,
	}
	headerData, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := output.Write(encryptedBackupMagic); err != nil {
		return err
	}
	if err := binary.Write(output, binary.BigEndian, uint32(len(headerData))); err != nil {
		return err
	}
	if _, err := output.Write(headerData); err != nil {
		return err
	}

	key := argon2.IDKey([]byte(password), salt, header.ArgonTime, header.ArgonMemory, header.ArgonThreads, 32)
	defer clear(key)
	gcm, err := newBackupGCM(key)
	if err != nil {
		return err
	}
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(writeSnapshotTar(snapshotDir, writer))
	}()
	defer reader.Close()

	buffer := make([]byte, header.ChunkSize)
	counter := uint32(0)
	for {
		count, readErr := io.ReadFull(reader, buffer)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if count > 0 {
			nonce := backupNonce(noncePrefix, counter)
			sealed := gcm.Seal(nil, nonce, buffer[:count], backupAAD(headerData, counter))
			if err := binary.Write(output, binary.BigEndian, uint32(len(sealed))); err != nil {
				return err
			}
			if _, err := output.Write(sealed); err != nil {
				return err
			}
			if counter == ^uint32(0) {
				return fmt.Errorf("备份内容过大")
			}
			counter++
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
	}
	terminator := gcm.Seal(nil, backupNonce(noncePrefix, counter), nil, backupAAD(headerData, counter))
	if err := binary.Write(output, binary.BigEndian, uint32(len(terminator))); err != nil {
		return err
	}
	_, err = output.Write(terminator)
	return err
}

func decryptBackupToDirectory(input io.Reader, password, targetDir string) (int, error) {
	reader := bufio.NewReader(input)
	magic := make([]byte, len(encryptedBackupMagic))
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != string(encryptedBackupMagic) {
		return 0, fmt.Errorf("不是 code-switch-R 加密备份")
	}
	var headerLength uint32
	if err := binary.Read(reader, binary.BigEndian, &headerLength); err != nil || headerLength == 0 || headerLength > encryptedBackupMaxHeader {
		return 0, fmt.Errorf("备份头无效")
	}
	headerData := make([]byte, headerLength)
	if _, err := io.ReadFull(reader, headerData); err != nil {
		return 0, err
	}
	var header encryptedBackupHeader
	if err := json.Unmarshal(headerData, &header); err != nil {
		return 0, err
	}
	if err := validateBackupHeader(header); err != nil {
		return 0, err
	}
	salt, err := base64.StdEncoding.DecodeString(header.Salt)
	if err != nil || len(salt) != 16 {
		return 0, fmt.Errorf("备份 salt 无效")
	}
	noncePrefix, err := base64.StdEncoding.DecodeString(header.NoncePrefix)
	if err != nil || len(noncePrefix) != 8 {
		return 0, fmt.Errorf("备份 nonce 无效")
	}
	key := argon2.IDKey([]byte(password), salt, header.ArgonTime, header.ArgonMemory, header.ArgonThreads, 32)
	defer clear(key)
	gcm, err := newBackupGCM(key)
	if err != nil {
		return 0, err
	}

	plainReader, plainWriter := io.Pipe()
	decryptDone := make(chan error, 1)
	go func() {
		decryptErr := decryptBackupChunks(reader, plainWriter, gcm, noncePrefix, headerData, header.ChunkSize)
		plainWriter.CloseWithError(decryptErr)
		decryptDone <- decryptErr
	}()
	count, extractErr := extractSnapshotTar(plainReader, targetDir)
	plainReader.CloseWithError(extractErr)
	decryptErr := <-decryptDone
	if extractErr != nil {
		return count, extractErr
	}
	return count, decryptErr
}

func validateBackupHeader(header encryptedBackupHeader) error {
	if header.Version != encryptedBackupVersion || header.KDF != "argon2id" || header.Cipher != "aes-256-gcm" {
		return fmt.Errorf("不支持的备份版本、KDF 或加密算法")
	}
	if header.ArgonTime < 1 || header.ArgonTime > 10 || header.ArgonMemory < 16*1024 || header.ArgonMemory > 256*1024 || header.ArgonThreads < 1 || header.ArgonThreads > 8 {
		return fmt.Errorf("备份 KDF 参数超出允许范围")
	}
	if header.ChunkSize < 64*1024 || header.ChunkSize > 8*1024*1024 {
		return fmt.Errorf("备份分块大小无效")
	}
	return nil
}

func decryptBackupChunks(reader io.Reader, writer io.Writer, gcm cipher.AEAD, noncePrefix, headerData []byte, chunkSize int) error {
	maxCiphertext := chunkSize + gcm.Overhead()
	for counter := uint32(0); ; counter++ {
		var length uint32
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return err
		}
		if int(length) > maxCiphertext || int(length) < gcm.Overhead() {
			return fmt.Errorf("备份分块长度无效")
		}
		sealed := make([]byte, length)
		if _, err := io.ReadFull(reader, sealed); err != nil {
			return err
		}
		plain, err := gcm.Open(nil, backupNonce(noncePrefix, counter), sealed, backupAAD(headerData, counter))
		if err != nil {
			return err
		}
		if len(plain) == 0 {
			var trailing [1]byte
			if count, readErr := reader.Read(trailing[:]); count != 0 || !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("备份结束标记后存在多余数据")
			}
			return nil
		}
		if _, err := writer.Write(plain); err != nil {
			return err
		}
		if counter == ^uint32(0) {
			return fmt.Errorf("备份分块计数溢出")
		}
	}
}

func newBackupGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func backupNonce(prefix []byte, counter uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[8:], counter)
	return nonce
}

func backupAAD(header []byte, counter uint32) []byte {
	aad := make([]byte, len(header)+4)
	copy(aad, header)
	binary.BigEndian.PutUint32(aad[len(header):], counter)
	return aad
}

func writeSnapshotTar(root string, output io.Writer) error {
	tarWriter := tar.NewWriter(output)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("备份不允许符号链接: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = tarWriter.Close()
		return err
	}
	return tarWriter.Close()
}

func extractSnapshotTar(input io.Reader, targetDir string) (int, error) {
	tarReader := tar.NewReader(input)
	count := 0
	var total int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		cleanName := filepath.Clean(filepath.FromSlash(header.Name))
		if cleanName == "." || filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
			return count, fmt.Errorf("备份包含非法路径: %s", header.Name)
		}
		target := filepath.Join(targetDir, cleanName)
		if !pathWithin(targetDir, target) {
			return count, fmt.Errorf("备份路径越界: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0o755); err != nil {
				return count, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || total+header.Size > encryptedBackupMaxFile {
				return count, fmt.Errorf("备份解压大小超出限制")
			}
			total += header.Size
			if err := EnsureDir(filepath.Dir(target)); err != nil {
				return count, err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0o600)
			if err != nil {
				return count, err
			}
			_, copyErr := io.CopyN(file, tarReader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return count, copyErr
			}
			if closeErr != nil {
				return count, closeErr
			}
			count++
		default:
			return count, fmt.Errorf("备份包含不支持的条目类型: %s", header.Name)
		}
	}
}
