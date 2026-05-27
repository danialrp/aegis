// SPDX-License-Identifier: AGPL-3.0-or-later

package ca_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/ca"
)

func TestOpenOrCreate_GeneratesThenReloads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := ca.OpenOrCreate(dir)
	require.NoError(t, err)
	require.NotEmpty(t, first.CACertPEM())

	second, err := ca.OpenOrCreate(dir)
	require.NoError(t, err)
	require.Equal(t, first.CACertPEM(), second.CACertPEM(),
		"reloading the same dir must reuse the existing CA")
}

func TestIssueAgentCert_RoundTrip(t *testing.T) {
	t.Parallel()

	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)

	bundle, err := c.IssueAgentCert(42)
	require.NoError(t, err)
	require.NotEmpty(t, bundle.CertPEM)
	require.NotEmpty(t, bundle.KeyPEM)
	require.Len(t, bundle.Fingerprint, 64) // sha256 hex

	// Cert + key parse as a tls.Certificate.
	tlsCert, err := tls.X509KeyPair(bundle.CertPEM, bundle.KeyPEM)
	require.NoError(t, err)
	require.NotNil(t, tlsCert.PrivateKey)

	// Cert verifies against the CA pool.
	parsed, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(t, err)

	opts := x509.VerifyOptions{
		Roots:     c.CertPool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	_, err = parsed.Verify(opts)
	require.NoError(t, err)

	// Server id extracts back out of the URI SAN.
	serverID, err := ca.ExtractServerID(parsed)
	require.NoError(t, err)
	require.EqualValues(t, 42, serverID)
}

func TestIssueServerCert_RoundTrip(t *testing.T) {
	t.Parallel()

	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)

	bundle, err := c.IssueServerCert("controller.local", []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	require.NoError(t, err)

	tlsCert, err := tls.X509KeyPair(bundle.CertPEM, bundle.KeyPEM)
	require.NoError(t, err)

	parsed, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(t, err)
	require.Equal(t, "controller.local", parsed.Subject.CommonName)
	require.Contains(t, parsed.DNSNames, "localhost")
	require.True(t, parsed.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")))

	opts := x509.VerifyOptions{
		Roots:     c.CertPool(),
		DNSName:   "localhost",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	_, err = parsed.Verify(opts)
	require.NoError(t, err)
}

func TestExtractServerID_RejectsForeignCerts(t *testing.T) {
	t.Parallel()

	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)
	bundle, err := c.IssueServerCert("controller.local", nil, nil)
	require.NoError(t, err)

	tlsCert, _ := tls.X509KeyPair(bundle.CertPEM, bundle.KeyPEM)
	parsed, _ := x509.ParseCertificate(tlsCert.Certificate[0])

	_, err = ca.ExtractServerID(parsed)
	require.Error(t, err, "server cert has no agent URI SAN; must not be acceptable as an agent identity")
}

func TestIssueAgentCert_RejectsBadServerID(t *testing.T) {
	t.Parallel()

	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)

	_, err = c.IssueAgentCert(0)
	require.Error(t, err)
	_, err = c.IssueAgentCert(-1)
	require.Error(t, err)
}
