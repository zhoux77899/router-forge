package control

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/netpolicy"
)

type networkChecker interface {
	CheckNetwork(context.Context, domain.Environment, domain.NetworkOperation) (domain.NetworkCheck, error)
}

func (s *Server) networkRoutes() {
	s.mux.HandleFunc("GET /api/network-profiles", s.admin(func(w http.ResponseWriter, r *http.Request) {
		profiles, err := s.store.NetworkProfiles(r.Context())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, profiles)
	}))
	s.mux.HandleFunc("POST /api/network-profiles", s.admin(func(w http.ResponseWriter, r *http.Request) {
		input, ok := decode[domain.NetworkProfileInput](w, r)
		if !ok {
			return
		}
		profile, err := s.store.SaveNetworkProfile(r.Context(), input)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, profile)
	}))
	s.mux.HandleFunc("POST /api/network-profiles/{id}/check", s.admin(s.checkNetwork))
}

func (s *Server) checkNetwork(w http.ResponseWriter, r *http.Request) {
	input, ok := decode[domain.NetworkCheckInput](w, r)
	if !ok {
		return
	}
	if input.ProfileRevision == "" || !domain.ValidNetworkURL(input.TargetURL, false) {
		problem(w, errors.New("select a saved network revision and an explicit credential-free HTTP(S) check target"))
		return
	}
	profile, err := s.store.NetworkProfileRevision(r.Context(), r.PathValue("id"), input.ProfileRevision)
	if err != nil {
		problem(w, err)
		return
	}
	if err = profile.ValidateSaved(); err != nil {
		problem(w, err)
		return
	}
	if profile.TLSMode == domain.NetworkSkipTLS && !input.ConfirmTLSRisk {
		problem(w, errors.New("this check requires independent TLS risk confirmation for the selected saved profile and target"))
		return
	}
	var credential *domain.ProxyCredential
	if profile.ProxyAuth == domain.NetworkBasicAuth {
		value, err := s.store.ProxyCredential(r.Context(), profile.ID, profile.CredentialRevision)
		if err != nil {
			problem(w, errors.New("selected proxy credential reference is unavailable"))
			return
		}
		credential = &value
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	operation := domain.NetworkOperation{Profile: profile, Credential: credential, TargetURL: input.TargetURL}
	var result domain.NetworkCheck
	if profile.Scope == domain.NetworkDesktop {
		result, err = checkDesktopNetwork(ctx, operation)
	} else {
		environment, lookupErr := s.store.Environment(ctx, profile.EnvironmentID)
		if lookupErr != nil {
			problem(w, lookupErr)
			return
		}
		checker, supported := s.executor.(networkChecker)
		if !supported {
			fail(w, http.StatusConflict, "installed worker cannot check this host-scoped network profile")
			return
		}
		result, err = checker.CheckNetwork(ctx, environment, operation)
	}
	if err != nil {
		problem(w, errors.New(safeNetworkError(err, credential)))
		return
	}
	if result.ProfileID != profile.ID || result.ProfileRevision != profile.Revision || result.TargetURL != input.TargetURL || result.ObservedAt == "" {
		fail(w, http.StatusConflict, "network check returned an unrelated or incomplete observation")
		return
	}
	if result.Issues == nil {
		result.Issues = []string{}
	}
	for i, issue := range result.Issues {
		result.Issues[i] = safeNetworkError(errors.New(issue), credential)
	}
	write(w, result)
}

func checkDesktopNetwork(ctx context.Context, operation domain.NetworkOperation) (domain.NetworkCheck, error) {
	profile := operation.Profile
	if profile.Scope != domain.NetworkDesktop {
		return domain.NetworkCheck{}, errors.New("Desktop cannot use an execution host's CA path or network profile")
	}
	policy := netpolicy.Policy{ProxyMode: string(profile.ProxyMode), ProxyURL: profile.ProxyURL, ProxyTargets: append([]string{}, profile.ProxyTargets...), NoProxy: profile.NoProxy, TLSMode: string(profile.TLSMode), TrustMode: string(profile.TrustMode), CACertPath: profile.CACertPath, TLSRiskAccepted: profile.TLSRiskAccepted}
	if profile.ProxyAuth == domain.NetworkBasicAuth {
		if operation.Credential == nil || operation.Credential.Validate() != nil {
			return domain.NetworkCheck{}, errors.New("proxy credential reference is unavailable")
		}
		policy.Auth = &netpolicy.Credentials{Username: operation.Credential.Username, Password: operation.Credential.Password}
	}
	client, err := netpolicy.NewClient(policy)
	if err != nil {
		return domain.NetworkCheck{}, errors.New(safeNetworkError(err, operation.Credential))
	}
	defer client.CloseIdleConnections()
	result := domain.NetworkCheck{ProfileID: profile.ID, ProfileRevision: profile.Revision, TargetURL: operation.TargetURL, ObservedAt: domain.Now(), Issues: []string{}}
	if transport, ok := client.Transport.(*netpolicy.Transport); ok {
		result.CASHA256 = transport.CACertSHA256()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, operation.TargetURL, nil)
	if err != nil {
		return domain.NetworkCheck{}, errors.New("invalid network check target")
	}
	response, err := client.Do(request)
	if err != nil {
		result.Issues = append(result.Issues, safeNetworkError(err, operation.Credential))
		result.ObservedAt = domain.Now()
		return result, nil
	}
	defer response.Body.Close()
	result.Reachable, result.HTTPStatus, result.ObservedAt = true, &response.StatusCode, domain.Now()
	if response.TLS != nil {
		verified := len(response.TLS.VerifiedChains) > 0
		result.TLSVerified = &verified
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		result.Issues = append(result.Issues, "redirect received; it was not followed")
	}
	if response.StatusCode >= 400 {
		result.Issues = append(result.Issues, "HTTP error response received; network reachability does not prove service authorization or readiness")
	}
	return result, nil
}

func safeNetworkError(err error, credential *domain.ProxyCredential) string {
	message := err.Error()
	if credential != nil {
		for _, secret := range []string{credential.Username, credential.Password} {
			if secret == "" {
				continue
			}
			if len(secret) < 5 {
				return "network operation failed; sensitive proxy diagnostics withheld"
			}
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	if len(message) > 2048 {
		message = message[:2048]
	}
	return message
}
