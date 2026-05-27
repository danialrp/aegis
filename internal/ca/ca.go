// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ca implements Aegis's internal certificate authority.
//
// The CA issues two flavours of certificate, both signed by the same
// root and both built on ECDSA P-256 (TLS 1.3 native, ~70-byte
// signatures, fast on every platform we care about):
//
//   - Server certs for the controller's mTLS agent endpoint (:8443),
//     authenticating the controller to dialing agents.
//
//   - Agent client certs, one per managed server, encoding the
//     numeric server ID as a SPIFFE-style URI SAN so the controller
//     can derive identity from the TLS handshake alone.
//
// The CA material lives at a configurable directory (default
// /var/lib/aegis/ca) as plain PEM files. Encryption-at-rest is
// deferred — the controller host is the trust root and the volume is
// expected to be operator-protected.
package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile  = "ca.crt"
	caKeyFile   = "ca.key"
	caKeyMode   = 0o600
	caCertMode  = 0o644
	dirFileMode = 0o700

	// CACertValidity is intentionally long — rotating the root is a
	// disruptive operation. Leaf certs do the regular rotation.
	CACertValidity = 10 * 365 * 24 * time.Hour

	// AgentCertValidity matches Let's Encrypt's 90-day cadence,
	// forcing the renewal path to be exercised regularly. Renewal
	// itself lands later; for 0.7 the issuance flow is enough.
	AgentCertValidity = 90 * 24 * time.Hour

	// ServerCertValidity for the controller's own TLS endpoint.
	// Shorter than the CA, longer than agent leaves — re-issued on
	// every controller restart so freshness is automatic.
	ServerCertValidity = 365 * 24 * time.Hour

	spiffeTrustDomain = "aegis"
)

// CA holds the loaded root key + cert. Construct via OpenOrCreate.
type CA struct {
	dir     string
	cert    *x509.Certificate
	certPEM []byte
	key     *ecdsa.PrivateKey
}

// Bundle is a PEM-encoded cert + key pair ready to be written to disk
// or loaded into a tls.Certificate.
type Bundle struct {
	CertPEM     []byte
	KeyPEM      []byte
	Fingerprint string // SHA-256 of the DER certificate, hex-encoded
}

// OpenOrCreate loads the CA from dir, creating it if absent. Safe to
// call on every startup — existing material is reused.
func OpenOrCreate(dir string) (*CA, error) {
	if err := os.MkdirAll(dir, dirFileMode); err != nil {
		return nil, fmt.Errorf("mkdir ca dir: %w", err)
	}

	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	if _, err := os.Stat(certPath); errors.Is(err, os.ErrNotExist) {
		return create(dir, certPath, keyPath)
	} else if err != nil {
		return nil, fmt.Errorf("stat ca cert: %w", err)
	}

	return load(dir, certPath, keyPath)
}

func create(dir, certPath, keyPath string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}

	now := time.Now().UTC()
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Aegis Internal CA",
			Organization: []string{"Aegis"},
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(CACertValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("self-sign ca: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse ca: %w", err)
	}

	certPEM := encodeCertPEM(der)
	keyPEM, err := encodeKeyPEM(key)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(certPath, certPEM, caCertMode); err != nil {
		return nil, fmt.Errorf("write ca.crt: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, caKeyMode); err != nil {
		return nil, fmt.Errorf("write ca.key: %w", err)
	}

	return &CA{dir: dir, cert: cert, certPEM: certPEM, key: key}, nil
}

func load(dir, certPath, keyPath string) (*CA, error) {
	// gosec G304: paths are constructed from the caller-supplied CA
	// directory (typically /var/lib/aegis/ca) joined with our fixed
	// filenames — not arbitrary user input.
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // controlled path
	if err != nil {
		return nil, fmt.Errorf("read ca.crt: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // controlled path
	if err != nil {
		return nil, fmt.Errorf("read ca.key: %w", err)
	}

	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, fmt.Errorf("parse ca.crt: %w", err)
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse ca.key: %w", err)
	}

	return &CA{dir: dir, cert: cert, certPEM: certPEM, key: key}, nil
}

// CACertPEM returns the PEM-encoded CA certificate, suitable for
// shipping to agents as their root-of-trust bundle.
func (c *CA) CACertPEM() []byte { return c.certPEM }

// CertPool returns an x509.CertPool seeded with the CA cert, for
// configuring tls.Config.ClientCAs on the agent endpoint.
func (c *CA) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

// IssueAgentCert mints a client certificate identifying the given
// server. The URI SAN is the canonical identity the controller reads
// off the verified TLS handshake.
func (c *CA) IssueAgentCert(serverID int64) (Bundle, error) {
	if serverID <= 0 {
		return Bundle{}, errors.New("serverID must be positive")
	}
	uri, err := url.Parse(fmt.Sprintf("spiffe://%s/server/%d", spiffeTrustDomain, serverID))
	if err != nil {
		return Bundle{}, fmt.Errorf("build spiffe uri: %w", err)
	}

	tmpl, err := baseLeafTemplate(AgentCertValidity)
	if err != nil {
		return Bundle{}, err
	}
	tmpl.Subject.CommonName = fmt.Sprintf("aegis-agent-%d", serverID)
	tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	tmpl.URIs = []*url.URL{uri}

	return c.signLeaf(tmpl)
}

// IssueServerCert mints a server certificate for the controller's
// own TLS endpoint. SANs typically include the public hostname and
// "localhost"/127.0.0.1 for local-dev.
func (c *CA) IssueServerCert(commonName string, dnsNames []string, ipSANs []net.IP) (Bundle, error) {
	tmpl, err := baseLeafTemplate(ServerCertValidity)
	if err != nil {
		return Bundle{}, err
	}
	tmpl.Subject.CommonName = commonName
	tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	tmpl.DNSNames = dnsNames
	tmpl.IPAddresses = ipSANs

	return c.signLeaf(tmpl)
}

// ExtractServerID returns the server id encoded in cert's SPIFFE URI
// SAN. Returns an error if the cert has no Aegis SPIFFE SAN or it is
// malformed — meaning the cert was not issued by us.
func ExtractServerID(cert *x509.Certificate) (int64, error) {
	for _, u := range cert.URIs {
		if u.Scheme != "spiffe" || u.Host != spiffeTrustDomain {
			continue
		}
		var id int64
		if _, err := fmt.Sscanf(u.Path, "/server/%d", &id); err != nil {
			continue
		}
		if id > 0 {
			return id, nil
		}
	}
	return 0, errors.New("no aegis server URI SAN on certificate")
}

// FingerprintHex returns the SHA-256 fingerprint of a DER certificate.
func FingerprintHex(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// --- internals ---

func (c *CA) signLeaf(tmpl *x509.Certificate) (Bundle, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Bundle{}, fmt.Errorf("generate leaf key: %w", err)
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return Bundle{}, fmt.Errorf("sign leaf: %w", err)
	}
	keyPEM, err := encodeKeyPEM(key)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{
		CertPEM:     encodeCertPEM(der),
		KeyPEM:      keyPEM,
		Fingerprint: FingerprintHex(der),
	}, nil
}

func baseLeafTemplate(validity time.Duration) (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
		Subject:               pkix.Name{Organization: []string{"Aegis"}},
	}, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("random serial: %w", err)
	}
	return n, nil
}

func encodeCertPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodeKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal ec key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), nil
}

func parseCertPEM(b []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKeyPEM(b []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}
