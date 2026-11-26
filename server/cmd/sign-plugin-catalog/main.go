package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return runWithKeys(os.Args[1:], plugins.ProductionCatalogKeys(), os.Stdout, os.Stderr)
}

func runWithKeys(args []string, keys []plugins.CatalogKey, stdout, stderr io.Writer) error {
	if stdout == nil || stderr == nil {
		return errors.New("catalog command output streams are required")
	}
	flags := flag.NewFlagSet("sign-plugin-catalog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var keyID, keyFile, indexPath, signaturePath string
	var verifyOnly bool
	flags.StringVar(&keyID, "key-id", "", "pinned signing key ID")
	flags.StringVar(&keyFile, "key-file", "", "PKCS#8 Ed25519 private key PEM file")
	flags.StringVar(&indexPath, "index", "", "raw plugin catalog JSON path")
	flags.StringVar(&signaturePath, "signature", "", "detached signature envelope path")
	flags.BoolVar(&verifyOnly, "verify", false, "verify the index and detached signature using production-pinned keys")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if indexPath == "" || signaturePath == "" {
		return errors.New("--index and --signature are required")
	}
	if !plugins.CatalogVerificationReady(keys) {
		return errors.New("current and next pinned catalog keys are required")
	}
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("read catalog index: %w", err)
	}
	if verifyOnly {
		if keyID != "" || keyFile != "" {
			return errors.New("--verify cannot be combined with signing-key arguments")
		}
		signature, err := os.ReadFile(signaturePath)
		if err != nil {
			return fmt.Errorf("read catalog signature: %w", err)
		}
		catalog, err := plugins.VerifyCatalog(index, signature, keys)
		if err != nil {
			return fmt.Errorf("verify catalog signature: %w", err)
		}
		if _, err := fmt.Fprintf(stdout, "verified catalog signed by %s (%d plugins)\n", catalog.SignerKeyID, len(catalog.Plugins)); err != nil {
			return fmt.Errorf("write verification result: %w", err)
		}
		return nil
	}
	if keyID == "" || keyFile == "" {
		return errors.New("--key-id and --key-file are required unless --verify is used")
	}
	privatePEM, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("read catalog signing key: %w", err)
	}
	block, rest := pem.Decode(privatePEM)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || !bytes.Equal(bytes.TrimSpace(privatePEM), bytes.TrimSpace(pem.EncodeToMemory(block))) {
		return errors.New("catalog signing key must contain exactly one PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse catalog signing key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return errors.New("catalog signing key is not Ed25519")
	}
	signature, err := plugins.SignCatalog(index, keyID, privateKey, keys)
	if err != nil {
		return fmt.Errorf("sign catalog: %w", err)
	}
	if err := os.WriteFile(signaturePath, signature, 0o644); err != nil {
		return fmt.Errorf("write catalog signature: %w", err)
	}
	if _, err := fmt.Fprintf(stdout, "signed catalog with pinned key %s\n", keyID); err != nil {
		return fmt.Errorf("write signing result: %w", err)
	}
	return nil
}
