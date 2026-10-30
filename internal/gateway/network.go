package gateway

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/netpolicy"
)

func freezeNetwork(lease *Lease) (*http.Client, error) {
	if lease.Network == nil || lease.Network.Scope != domain.NetworkDesktop {
		return nil, errors.New("model gateway requires an explicit Desktop network profile")
	}
	profile := *lease.Network
	profile.ProxyTargets = append([]string(nil), profile.ProxyTargets...)
	if err := profile.ValidateSaved(); err != nil {
		return nil, err
	}
	policy := netpolicy.Policy{
		ProxyMode: string(profile.ProxyMode), ProxyURL: profile.ProxyURL,
		ProxyTargets: profile.ProxyTargets, NoProxy: profile.NoProxy,
		TLSMode: string(profile.TLSMode), TrustMode: string(profile.TrustMode),
		CACertPath: profile.CACertPath, TLSRiskAccepted: profile.TLSRiskAccepted,
	}
	if profile.ProxyAuth == domain.NetworkBasicAuth {
		if lease.ProxyCredential == nil {
			return nil, errors.New("approved model proxy credential is unavailable")
		}
		credential := *lease.ProxyCredential
		if err := credential.Validate(); err != nil {
			return nil, err
		}
		lease.ProxyCredential = &credential
		policy.Auth = &netpolicy.Credentials{Username: credential.Username, Password: credential.Password}
	} else if lease.ProxyCredential != nil || profile.HasCredentials || profile.CredentialRevision != "" {
		return nil, errors.New("model proxy credentials require explicit Basic proxy authentication")
	}
	transport, err := netpolicy.NewTransport(policy)
	if err != nil {
		return nil, err
	}
	lease.Network = &profile
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func (lease *leaseState) close() {
	lease.mu.Lock()
	lease.revoked = true
	for _, cancel := range lease.active {
		cancel()
	}
	lease.mu.Unlock()
	lease.client.CloseIdleConnections()
}

func (Lease) String() string   { return "gateway.Lease" }
func (Lease) GoString() string { return "gateway.Lease" }

func containsLeaseSecret(value string, lease Lease, caseInsensitive bool) bool {
	secrets := []string{lease.Token, lease.APIKey}
	if credential := lease.ProxyCredential; credential != nil {
		encoded := base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password))
		secrets = append(secrets, credential.Username, credential.Password, encoded, strings.TrimRight(encoded, "="))
	}
	if caseInsensitive {
		value = strings.ToLower(value)
	}
	for _, secret := range secrets {
		if caseInsensitive {
			secret = strings.ToLower(secret)
		}
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}
