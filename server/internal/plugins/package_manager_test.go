package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type recordedCommand struct {
	program string
	args    []string
}

type recordingCommandRunner struct {
	calls []recordedCommand
}

func (r *recordingCommandRunner) Run(_ context.Context, program string, args ...string) error {
	r.calls = append(r.calls, recordedCommand{program: program, args: append([]string(nil), args...)})
	return nil
}

type fixedMetadataReader struct {
	metadata PackageMetadata
	err      error
}

func (r fixedMetadataReader) Read(_ context.Context, _ string, _ string, _ string, _ string, _ string) (PackageMetadata, error) {
	return r.metadata, r.err
}

func makeTestVerifiedArtifact(t *testing.T, format, id, version, arch string) VerifiedArtifact {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin-package."+format)
	data := []byte("verified package payload")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	artifact := VerifiedArtifact{ID: id, Version: version, Arch: arch, AppVersion: "dev", Format: format, Path: path, SHA256: hex.EncodeToString(digest[:]), verified: true}
	artifact.seal()
	return artifact
}

func TestProductionInstallerBlocksMutationsWithoutReleaseKeys(t *testing.T) {
	installer := NewPackageInstaller("/opt/etc/mounvix/config.json")
	artifact := makeTestVerifiedArtifact(t, "ipk", "demo", "1.0.0", "aarch64")
	if err := installer.Install(context.Background(), "demo", artifact); err == nil {
		t.Fatal("production installer enabled without release keys and signed catalog")
	}
	if err := installer.Remove(context.Background(), "demo"); err == nil {
		t.Fatal("production package removal enabled while catalog is read-only")
	}
}

func TestPackageInstallerUsesFixedPlatformCommands(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
		format     string
		manager    string
		install    []string
		remove     []string
	}{
		{"Entware", "/opt/etc/mounvix/config.json", "ipk", "/opt/bin/opkg", []string{"install"}, []string{"remove", "demo"}},
		{"Debian", "/etc/mounvix/config.json", "deb", "/usr/bin/dpkg", []string{"--install"}, []string{"--remove", "demo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingCommandRunner{}
			metadata := fixedMetadataReader{metadata: PackageMetadata{Package: "demo", Version: "1.0.0", Architecture: "aarch64", PluginID: "demo", PluginVersion: "1.0.0"}}
			installer := newPackageInstaller(tt.configPath, runner, metadata)
			artifact := makeTestVerifiedArtifact(t, tt.format, "demo", "1.0.0", "aarch64")
			if err := installer.Install(context.Background(), "demo", artifact); err != nil {
				t.Fatalf("Install(): %v", err)
			}
			if err := installer.Remove(context.Background(), "demo"); err != nil {
				t.Fatalf("Remove(): %v", err)
			}
			if len(runner.calls) != 2 {
				t.Fatalf("commands=%+v, want install and remove", runner.calls)
			}
			if runner.calls[0].program != tt.manager || !reflect.DeepEqual(runner.calls[0].args, append(tt.install, artifact.Path)) {
				t.Fatalf("install command=%+v", runner.calls[0])
			}
			if runner.calls[1].program != tt.manager || !reflect.DeepEqual(runner.calls[1].args, tt.remove) {
				t.Fatalf("remove command=%+v", runner.calls[1])
			}
		})
	}
}

func TestPackageInstallerRejectsUnsafeIDsAndUnverifiedArtifacts(t *testing.T) {
	runner := &recordingCommandRunner{}
	metadata := fixedMetadataReader{metadata: PackageMetadata{Package: "demo", Version: "1.0.0", Architecture: "aarch64", PluginID: "demo", PluginVersion: "1.0.0"}}
	installer := newPackageInstaller("/opt/etc/mounvix/config.json", runner, metadata)
	artifact := makeTestVerifiedArtifact(t, "ipk", "demo", "1.0.0", "aarch64")

	tests := []struct {
		name     string
		id       string
		artifact VerifiedArtifact
	}{
		{"shell metacharacters", "demo;touch /tmp/pwned", artifact},
		{"reserved builtin", "rclone", artifact},
		{"catalog ID mismatch", "other", artifact},
		{"unverified artifact", "demo", VerifiedArtifact{ID: "demo", Version: "1.0.0", Arch: "aarch64", Format: "ipk", Path: artifact.Path}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := installer.Install(context.Background(), tt.id, tt.artifact); err == nil {
				t.Fatal("unsafe package installation accepted")
			}
		})
	}
	if err := installer.Remove(context.Background(), "demo;touch /tmp/pwned"); err == nil {
		t.Fatal("unsafe removal ID accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner was called for rejected input: %+v", runner.calls)
	}
}

func TestPackageInstallerRejectsRepointedVerifiedArtifact(t *testing.T) {
	runner := &recordingCommandRunner{}
	metadata := fixedMetadataReader{metadata: PackageMetadata{Package: "demo", Version: "1.0.0", Architecture: "aarch64", PluginID: "demo", PluginVersion: "1.0.0"}}
	installer := newPackageInstaller("/opt/etc/mounvix/config.json", runner, metadata)
	artifact := makeTestVerifiedArtifact(t, "ipk", "demo", "1.0.0", "aarch64")
	replacement := filepath.Join(t.TempDir(), "replacement.ipk")
	contents, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact.Path = replacement
	if err := installer.Install(context.Background(), "demo", artifact); err == nil {
		t.Fatal("verified artifact path could be replaced after verification")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("package manager ran for a mutated artifact: %+v", runner.calls)
	}
}

func TestPackageInstallerRejectsManifestIdentityMismatchBeforeRunner(t *testing.T) {
	runner := &recordingCommandRunner{}
	metadata := fixedMetadataReader{metadata: PackageMetadata{Package: "demo", Version: "1.0.0", Architecture: "aarch64", PluginID: "different", PluginVersion: "1.0.0"}}
	installer := newPackageInstaller("/opt/etc/mounvix/config.json", runner, metadata)
	artifact := makeTestVerifiedArtifact(t, "ipk", "demo", "1.0.0", "aarch64")
	if err := installer.Install(context.Background(), "demo", artifact); err == nil {
		t.Fatal("package with a different embedded manifest ID was accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("package manager ran before manifest identity validation: %+v", runner.calls)
	}
}

func TestPackageInstallerRejectsControlMetadataMismatchBeforeRunner(t *testing.T) {
	runner := &recordingCommandRunner{}
	installer := newPackageInstaller("/opt/etc/mounvix/config.json", runner, fixedMetadataReader{metadata: PackageMetadata{Package: "other", Version: "9.9.9", Architecture: "mips", PluginID: "other", PluginVersion: "9.9.9"}})
	artifact := makeTestVerifiedArtifact(t, "ipk", "demo", "1.0.0", "aarch64")
	if err := installer.Install(context.Background(), "demo", artifact); err == nil {
		t.Fatal("package control metadata mismatch accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("package manager ran before metadata validation: %+v", runner.calls)
	}
}

func TestPackageInstallerDisablesUnknownPlatformAndWrongFormat(t *testing.T) {
	for _, tt := range []struct {
		name            string
		configPath      string
		format          string
		removeSupported bool
	}{
		{"unknown config path", "/tmp/mounvix/config.json", "ipk", false},
		{"wrong package format", "/opt/etc/mounvix/config.json", "deb", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingCommandRunner{}
			installer := newPackageInstaller(tt.configPath, runner, fixedMetadataReader{metadata: PackageMetadata{Package: "demo", Version: "1.0.0", Architecture: "aarch64", PluginID: "demo", PluginVersion: "1.0.0"}})
			artifact := makeTestVerifiedArtifact(t, tt.format, "demo", "1.0.0", "aarch64")
			if err := installer.Install(context.Background(), "demo", artifact); err == nil {
				t.Fatal("unsupported platform/package format accepted")
			}
			removeErr := installer.Remove(context.Background(), "demo")
			if tt.removeSupported {
				if removeErr != nil {
					t.Fatalf("remove on recognized platform: %v", removeErr)
				}
				if len(runner.calls) != 1 || runner.calls[0].program != "/opt/bin/opkg" || !reflect.DeepEqual(runner.calls[0].args, []string{"remove", "demo"}) {
					t.Fatalf("unexpected remove command: %+v", runner.calls)
				}
				return
			}
			if removeErr == nil {
				t.Fatal("package removal accepted on unknown platform")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runner was called on unknown platform: %+v", runner.calls)
			}
		})
	}
}

func TestReadPackageDataRejectsSymlinks(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	header := &tar.Header{Name: "opt/etc/mounvix/plugins/demo/evil", Typeflag: tar.TypeSymlink, Linkname: "../../../../etc/passwd", Mode: 0o777}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"demo plugin","executable":"bin/demo"}`)
	if err := writer.WriteHeader(&tar.Header{Name: "opt/etc/mounvix/plugins/demo/plugin.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readPluginManifestTar(&archive, "demo", "ipk"); err == nil {
		t.Fatal("package data symlink was accepted")
	}
}

func TestReadIPKControlAndManifestMetadata(t *testing.T) {
	control := []byte("Package: demo\nVersion: 1.0.0\nArchitecture: aarch64\n")
	manifest := []byte(`{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"demo plugin","executable":"bin/demo"}`)
	controlTarGz := makeTarGz(t, "control", control)
	dataTarGz := makeTarGz(t, "./opt/etc/mounvix/plugins/demo/plugin.json", manifest)
	ipk := makeTarGzEntries(t, tarEntry{name: "./debian-binary", contents: []byte("2.0\n")}, tarEntry{name: "./control.tar.gz", contents: controlTarGz}, tarEntry{name: "./data.tar.gz", contents: dataTarGz})
	artifactPath := filepath.Join(t.TempDir(), "demo.ipk")
	if err := os.WriteFile(artifactPath, ipk, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := (systemPackageMetadataReader{}).Read(context.Background(), artifactPath, "ipk", "demo", "dev", "amd64")
	if err != nil {
		t.Fatalf("read IPK metadata: %v", err)
	}
	if metadata != (PackageMetadata{Package: "demo", Version: "1.0.0", Architecture: "aarch64", PluginID: "demo", PluginVersion: "1.0.0"}) {
		t.Fatalf("metadata=%+v", metadata)
	}
}

type tarEntry struct {
	name     string
	contents []byte
}

func makeTarGz(t *testing.T, name string, contents []byte) []byte {
	return makeTarGzEntries(t, tarEntry{name: name, contents: contents})
}

func makeTarGzEntries(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o600, Size: int64(len(entry.contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestReadPackageControlRejectsTraversal(t *testing.T) {
	control := []byte("Package: demo\nVersion: 1.0.0\nArchitecture: aarch64\n")
	controlTarGz := makeTarGz(t, "../control", control)
	dataTarGz := makeTarGzEntries(t)
	ipk := makeTarGzEntries(t, tarEntry{name: "./debian-binary", contents: []byte("2.0\n")}, tarEntry{name: "./control.tar.gz", contents: controlTarGz}, tarEntry{name: "./data.tar.gz", contents: dataTarGz})
	path := filepath.Join(t.TempDir(), "bad.ipk")
	if err := os.WriteFile(path, ipk, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (systemPackageMetadataReader{}).Read(context.Background(), path, "ipk", "demo", "dev", "amd64"); err == nil {
		t.Fatal("control archive traversal accepted")
	}
}
