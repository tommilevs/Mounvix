package plugins

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	maxPackageControlBytes      = 1 << 20
	maxPackageExpandedDataBytes = 512 << 20
	legacyTarRegularTypeflag    = byte(0)
)

func isRegularTarFile(header *tar.Header) bool {
	return header.Typeflag == tar.TypeReg || header.Typeflag == legacyTarRegularTypeflag
}

// PackageMetadata is the package-manager control identity checked before installation.
type PackageMetadata struct {
	Package       string
	Version       string
	Architecture  string
	PluginID      string
	PluginVersion string
}

// PackageMetadataReader reads package and embedded plugin identity without mutating installed state.
type PackageMetadataReader interface {
	Read(ctx context.Context, artifactPath, format, pluginID, appVersion, runtimeArch string) (PackageMetadata, error)
}

// CommandRunner executes one fixed package-manager binary with argument-separated input.
type CommandRunner interface {
	Run(ctx context.Context, program string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, program string, args ...string) error {
	command := exec.CommandContext(ctx, program, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("package manager command failed: %w", err)
	}
	return nil
}

type systemPackageMetadataReader struct{}

func (systemPackageMetadataReader) Read(ctx context.Context, artifactPath, format, pluginID, appVersion, runtimeArch string) (PackageMetadata, error) {
	if ctx == nil {
		return PackageMetadata{}, errors.New("package metadata context is required")
	}
	if !validInstallID(pluginID) {
		return PackageMetadata{}, errors.New("expected plugin ID is invalid")
	}
	var controlBytes, manifestBytes []byte
	var err error
	switch format {
	case "ipk":
		controlBytes, manifestBytes, err = readIPKMetadataFiles(artifactPath, pluginID)
		if err != nil {
			return PackageMetadata{}, err
		}
	case "deb":
		command := exec.CommandContext(ctx, "/usr/bin/dpkg-deb", "--field", artifactPath)
		controlBytes, err = command.Output()
		if err != nil {
			return PackageMetadata{}, fmt.Errorf("read Debian package control metadata: %w", err)
		}
		manifestBytes, err = readDebPluginManifest(ctx, artifactPath, pluginID)
		if err != nil {
			return PackageMetadata{}, err
		}
	default:
		return PackageMetadata{}, fmt.Errorf("unsupported package format %q", format)
	}
	metadata, err := parsePackageControl(controlBytes)
	if err != nil {
		return PackageMetadata{}, err
	}
	manifest, err := ParseManifest(bytes.NewReader(manifestBytes))
	if err != nil {
		return PackageMetadata{}, fmt.Errorf("parse embedded plugin manifest: %w", err)
	}
	if manifest.ID != pluginID || manifest.Version != metadata.Version {
		return PackageMetadata{}, errors.New("embedded plugin manifest identity does not match package control metadata")
	}
	if err := manifest.Validate(appVersion, runtimeArch); err != nil {
		return PackageMetadata{}, fmt.Errorf("validate embedded plugin manifest: %w", err)
	}
	metadata.PluginID = manifest.ID
	metadata.PluginVersion = manifest.Version
	return metadata, nil
}

func readIPKMetadataFiles(artifactPath, pluginID string) (controlBytes, manifestBytes []byte, resultErr error) {
	file, err := os.Open(artifactPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open IPK artifact: %w", err)
	}
	outerGzip, err := gzip.NewReader(file)
	if err != nil {
		closeErr := file.Close()
		return nil, nil, errors.Join(fmt.Errorf("open IPK tarball: %w", err), wrapArchiveCloseError(closeErr))
	}
	defer func() {
		closeErr := errors.Join(outerGzip.Close(), file.Close())
		if closeErr != nil {
			wrapped := wrapArchiveCloseError(closeErr)
			if resultErr == nil {
				resultErr = wrapped
			} else {
				resultErr = errors.Join(resultErr, wrapped)
			}
		}
	}()
	limitedOuter := &io.LimitedReader{R: outerGzip, N: maxPackageExpandedDataBytes + 1}
	outerTar := tar.NewReader(limitedOuter)
	var controlArchive, dataArchive []byte
	debianBinaryFound := false
	for {
		header, nextErr := outerTar.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, nil, fmt.Errorf("read IPK archive: %w", nextErr)
		}
		if err := validateArchiveMember(header.Name); err != nil {
			return nil, nil, fmt.Errorf("unsafe IPK archive member: %w", err)
		}
		member := path.Base(path.Clean(header.Name))
		if member == "debian-binary" {
			if debianBinaryFound || header.Size <= 0 || header.Size > 16 {
				return nil, nil, errors.New("IPK debian-binary metadata is invalid or duplicated")
			}
			contents, readErr := io.ReadAll(io.LimitReader(outerTar, header.Size+1))
			if readErr != nil || int64(len(contents)) != header.Size || strings.TrimSpace(string(contents)) != "2.0" {
				return nil, nil, errors.New("IPK debian-binary version is invalid")
			}
			debianBinaryFound = true
			continue
		}
		if member != "control.tar.gz" && member != "data.tar.gz" {
			continue
		}
		if header.Size < 0 || header.Size > maxCatalogArtifactBytes {
			return nil, nil, fmt.Errorf("IPK %s exceeds size limit", member)
		}
		contents, readErr := io.ReadAll(io.LimitReader(outerTar, header.Size+1))
		if readErr != nil {
			return nil, nil, fmt.Errorf("read IPK %s: %w", member, readErr)
		}
		if int64(len(contents)) != header.Size {
			return nil, nil, fmt.Errorf("IPK %s size mismatch", member)
		}
		switch member {
		case "control.tar.gz":
			if controlArchive != nil {
				return nil, nil, errors.New("IPK contains duplicate control archives")
			}
			controlArchive = contents
		case "data.tar.gz":
			if dataArchive != nil {
				return nil, nil, errors.New("IPK contains duplicate data archives")
			}
			dataArchive = contents
		}
	}
	if limitedOuter.N == 0 {
		return nil, nil, errors.New("IPK archive exceeds expanded size limit")
	}
	if !debianBinaryFound || controlArchive == nil || dataArchive == nil {
		return nil, nil, errors.New("IPK debian-binary, control, or data archive is missing")
	}
	controlBytes, err = readControlTarGzip(controlArchive)
	if err != nil {
		return nil, nil, err
	}
	manifestBytes, err = readPluginManifestTarGzip(dataArchive, pluginID)
	if err != nil {
		return nil, nil, err
	}
	return controlBytes, manifestBytes, nil
}

func readPluginManifestTarGzip(data []byte, pluginID string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open package data tarball: %w", err)
	}
	manifest, readErr := readPluginManifestTar(gzipReader, pluginID, "ipk")
	closeErr := gzipReader.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, wrapArchiveCloseError(closeErr))
	}
	return manifest, nil
}

func readDebPluginManifest(ctx context.Context, artifactPath, pluginID string) ([]byte, error) {
	command := exec.CommandContext(ctx, "/usr/bin/dpkg-deb", "--fsys-tarfile", artifactPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Debian package data stream: %w", err)
	}
	if err := command.Start(); err != nil {
		closeErr := stdout.Close()
		return nil, errors.Join(fmt.Errorf("start Debian package data reader: %w", err), wrapArchiveCloseError(closeErr))
	}
	manifest, readErr := readPluginManifestTar(stdout, pluginID, "deb")
	closeErr := stdout.Close()
	waitErr := command.Wait()
	if readErr != nil || closeErr != nil || waitErr != nil {
		return nil, errors.Join(wrapArchiveReadError(readErr), wrapArchiveCloseError(closeErr), wrapArchiveCommandError(waitErr))
	}
	return manifest, nil
}

func readPluginManifestTar(reader io.Reader, pluginID, format string) ([]byte, error) {
	if !validInstallID(pluginID) {
		return nil, errors.New("expected plugin ID is invalid")
	}
	var expectedPath string
	switch format {
	case "ipk":
		expectedPath = path.Join("opt", "etc", "mounvix", "plugins", pluginID, "plugin.json")
	case "deb":
		expectedPath = path.Join("etc", "mounvix", "plugins", pluginID, "plugin.json")
	default:
		return nil, fmt.Errorf("unsupported package format %q", format)
	}
	limited := &io.LimitedReader{R: reader, N: maxPackageExpandedDataBytes + 1}
	archive := tar.NewReader(limited)
	var manifest []byte
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read package data archive: %w", err)
		}
		if err := validateArchiveMember(header.Name); err != nil {
			return nil, fmt.Errorf("unsafe package data member: %w", err)
		}
		if header.Typeflag != tar.TypeDir && !isRegularTarFile(header) {
			return nil, errors.New("package data contains an unsupported file type")
		}
		if header.Size < 0 || header.Size > maxPackageExpandedDataBytes {
			return nil, errors.New("package data member exceeds size limit")
		}
		member := path.Clean(strings.TrimPrefix(header.Name, "./"))
		if member != expectedPath {
			continue
		}
		if !isRegularTarFile(header) {
			return nil, errors.New("embedded plugin manifest is not a regular file")
		}
		if manifest != nil {
			return nil, errors.New("package contains duplicate plugin manifests")
		}
		if header.Size > maxManifestBytes {
			return nil, errors.New("embedded plugin manifest exceeds size limit")
		}
		manifest, err = io.ReadAll(io.LimitReader(archive, header.Size+1))
		if err != nil {
			return nil, fmt.Errorf("read embedded plugin manifest: %w", err)
		}
		if int64(len(manifest)) != header.Size {
			return nil, errors.New("embedded plugin manifest size mismatch")
		}
	}
	if limited.N == 0 {
		return nil, errors.New("package data archive exceeds expanded size limit")
	}
	if manifest == nil {
		return nil, errors.New("embedded plugin manifest is missing")
	}
	return manifest, nil
}

func wrapArchiveReadError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("read package archive: %w", err)
}

func wrapArchiveCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close package archive: %w", err)
}

func wrapArchiveCommandError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("package archive command failed: %w", err)
}

func readControlTarGzip(data []byte) (contents []byte, resultErr error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open package control tarball: %w", err)
	}
	defer func() {
		if closeErr := gzipReader.Close(); closeErr != nil {
			wrapped := wrapArchiveCloseError(closeErr)
			if resultErr == nil {
				resultErr = wrapped
			} else {
				resultErr = errors.Join(resultErr, wrapped)
			}
		}
	}()
	limitedControl := &io.LimitedReader{R: gzipReader, N: maxPackageControlBytes + 1}
	archive := tar.NewReader(limitedControl)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read package control tarball: %w", err)
		}
		if err := validateArchiveMember(header.Name); err != nil {
			return nil, fmt.Errorf("unsafe package control member: %w", err)
		}
		if path.Clean(strings.TrimPrefix(header.Name, "./")) != "control" {
			continue
		}
		if contents != nil {
			return nil, errors.New("package control file is duplicated")
		}
		if !isRegularTarFile(header) {
			return nil, errors.New("package control member is not a regular file")
		}
		if header.Size < 0 || header.Size > maxPackageControlBytes {
			return nil, errors.New("package control file exceeds size limit")
		}
		contents, err = io.ReadAll(io.LimitReader(archive, header.Size+1))
		if err != nil {
			return nil, fmt.Errorf("read package control file: %w", err)
		}
		if int64(len(contents)) != header.Size {
			return nil, errors.New("package control file size mismatch")
		}
	}
	if limitedControl.N == 0 {
		return nil, errors.New("package control tarball exceeds expanded size limit")
	}
	if contents == nil {
		return nil, errors.New("package control file is missing")
	}
	return contents, nil
}

func validateArchiveMember(name string) error {
	if name == "" || strings.Contains(name, "\\") || strings.Contains(name, "\x00") || path.IsAbs(name) {
		return errors.New("invalid archive path")
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("archive path traverses outside package")
	}
	return nil
}

func parsePackageControl(contents []byte) (PackageMetadata, error) {
	if len(contents) == 0 || len(contents) > maxPackageControlBytes {
		return PackageMetadata{}, errors.New("package control metadata size is invalid")
	}
	metadata := PackageMetadata{}
	values := make(map[string]string, 3)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maxPackageControlBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			return PackageMetadata{}, errors.New("malformed package control field")
		}
		field = strings.ToLower(strings.TrimSpace(field))
		value = strings.TrimSpace(value)
		if field == "package" || field == "version" || field == "architecture" {
			if value == "" || values[field] != "" {
				return PackageMetadata{}, fmt.Errorf("package control field %q is missing or duplicated", field)
			}
			values[field] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return PackageMetadata{}, fmt.Errorf("scan package control metadata: %w", err)
	}
	metadata = PackageMetadata{Package: values["package"], Version: values["version"], Architecture: values["architecture"]}
	if metadata.Package == "" || metadata.Version == "" || metadata.Architecture == "" {
		return PackageMetadata{}, errors.New("package control metadata is incomplete")
	}
	return metadata, nil
}

// PackageInstaller performs fixed opkg/dpkg actions for verified artifacts only.
type PackageInstaller struct {
	mu               sync.RWMutex
	managerPath      string
	format           string
	runner           CommandRunner
	metadata         PackageMetadataReader
	mutationsEnabled bool
	removalEnabled   bool
}

// NewPackageInstaller selects the only supported package manager for the exact config path.
func NewPackageInstaller(configPath string) *PackageInstaller {
	installer := newPackageInstaller(configPath, execCommandRunner{}, systemPackageMetadataReader{})
	installer.mutationsEnabled = !DefaultCatalogStatus().ReadOnly
	installer.removalEnabled = installer.managerPath != ""
	return installer
}

func newPackageInstaller(configPath string, runner CommandRunner, metadata PackageMetadataReader) *PackageInstaller {
	installer := &PackageInstaller{runner: runner, metadata: metadata, mutationsEnabled: true}
	switch filepath.Clean(configPath) {
	case "/opt/etc/mounvix/config.json":
		installer.managerPath = "/opt/bin/opkg"
		installer.format = "ipk"
	case "/etc/mounvix/config.json":
		installer.managerPath = "/usr/bin/dpkg"
		installer.format = "deb"
	}
	installer.removalEnabled = installer.managerPath != ""
	return installer
}

// SetCatalogVerified enables installation only after the current signed catalog is verified.
func (installer *PackageInstaller) SetCatalogVerified(verified bool) {
	if installer == nil {
		return
	}
	installer.mu.Lock()
	installer.mutationsEnabled = verified && installer.managerPath != "" && installer.runner != nil && installer.metadata != nil
	installer.mu.Unlock()
}

// CanInstall reports whether a verified catalog and supported package manager permit installation.
func (installer *PackageInstaller) CanInstall() bool {
	if installer == nil {
		return false
	}
	installer.mu.RLock()
	enabled := installer.mutationsEnabled
	installer.mu.RUnlock()
	return enabled && installer.managerPath != "" && installer.runner != nil && installer.metadata != nil
}

// InstallFormat reports the package format accepted by this platform's fixed package manager.
func (installer *PackageInstaller) InstallFormat() (string, bool) {
	if installer == nil || installer.managerPath == "" || installer.runner == nil || installer.metadata == nil {
		return "", false
	}
	return installer.format, true
}

// InstallCatalogPackage downloads and installs only a catalog-bound, hash-verified artifact.
func (installer *PackageInstaller) InstallCatalogPackage(ctx context.Context, verifiedID string, asset CatalogAsset) error {
	if !installer.CanInstall() {
		return errors.New("package mutations are disabled for this platform")
	}
	if ctx == nil {
		return errors.New("package install context is required")
	}
	if !validInstallID(verifiedID) || asset.ID != verifiedID || !asset.trusted() {
		return errors.New("package ID or catalog asset is not verified")
	}
	artifact, err := FetchVerifiedArtifact(ctx, asset)
	if err != nil {
		return fmt.Errorf("fetch verified catalog package: %w", err)
	}
	installErr := installer.Install(ctx, verifiedID, artifact)
	if installErr != nil {
		installErr = fmt.Errorf("install verified package: %w", installErr)
	}
	cleanupErr := artifact.Cleanup()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("remove downloaded package artifact: %w", cleanupErr)
	}
	return errors.Join(installErr, cleanupErr)
}

// Install validates signed identity and package control metadata before invoking the manager.
func (installer *PackageInstaller) Install(ctx context.Context, verifiedID string, artifact VerifiedArtifact) error {
	if !installer.CanInstall() {
		return errors.New("package mutations are disabled for this platform")
	}
	if ctx == nil {
		return errors.New("package install context is required")
	}
	if !validInstallID(verifiedID) || !artifact.trusted() || artifact.ID != verifiedID {
		return errors.New("package ID or artifact is not verified")
	}
	if artifact.Format != installer.format {
		return errors.New("package format is not supported on this platform")
	}
	if artifact.Path == "" || !filepath.IsAbs(artifact.Path) {
		return errors.New("verified artifact path is invalid")
	}
	info, err := os.Lstat(artifact.Path)
	if err != nil {
		return fmt.Errorf("stat verified artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("verified artifact is not a regular file")
	}
	digest, err := hashFile(artifact.Path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(digest, artifact.SHA256) {
		return errors.New("verified artifact changed after download")
	}
	release, err := SharedProcessGate().Acquire(ctx, true)
	if err != nil {
		return fmt.Errorf("acquire package process gate: %w", err)
	}
	defer release()
	metadata, err := installer.metadata.Read(ctx, artifact.Path, artifact.Format, verifiedID, artifact.AppVersion, runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("read package metadata: %w", err)
	}
	if metadata.Package != verifiedID || metadata.Version != artifact.Version || !catalogArchEqual(metadata.Architecture, artifact.Arch) || metadata.PluginID != verifiedID || metadata.PluginVersion != artifact.Version {
		return errors.New("package control or embedded manifest identity does not match signed catalog metadata")
	}
	var args []string
	switch installer.format {
	case "ipk":
		args = []string{"install", artifact.Path}
	case "deb":
		args = []string{"--install", artifact.Path}
	default:
		return errors.New("package mutations are disabled for this platform")
	}
	if err := installer.runner.Run(ctx, installer.managerPath, args...); err != nil {
		return fmt.Errorf("install verified package: %w", err)
	}
	return nil
}

// CanRemove reports whether one validated package ID can be removed on this platform.
func (installer *PackageInstaller) CanRemove() bool {
	return installer != nil && installer.removalEnabled && installer.managerPath != "" && installer.runner != nil
}

// Remove removes one validated package ID using only the platform's fixed manager command.
func (installer *PackageInstaller) Remove(ctx context.Context, verifiedID string) error {
	if installer == nil || !installer.removalEnabled || installer.managerPath == "" || installer.runner == nil {
		return errors.New("package mutations are disabled for this platform")
	}
	if ctx == nil {
		return errors.New("package removal context is required")
	}
	if !validInstallID(verifiedID) {
		return errors.New("package ID is invalid")
	}
	var args []string
	switch installer.format {
	case "ipk":
		args = []string{"remove", verifiedID}
	case "deb":
		args = []string{"--remove", verifiedID}
	default:
		return errors.New("package mutations are disabled for this platform")
	}
	release, err := SharedProcessGate().Acquire(ctx, true)
	if err != nil {
		return fmt.Errorf("acquire package process gate: %w", err)
	}
	defer release()
	if err := installer.runner.Run(ctx, installer.managerPath, args...); err != nil {
		return fmt.Errorf("remove verified package: %w", err)
	}
	return nil
}

func validInstallID(id string) bool {
	return pluginIDPattern.MatchString(id) && id != "rclone"
}

func hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open verified artifact: %w", err)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("hash verified artifact: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close verified artifact: %w", closeErr)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
