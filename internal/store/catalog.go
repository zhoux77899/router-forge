package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func (s *Store) seed(ctx context.Context) error {
	suite := domain.SWEBenchVerified()
	b, _ := json.Marshal(suite)
	_, err := s.DB.ExecContext(ctx, "INSERT INTO evaluation_suites(id,config_json) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET config_json=excluded.config_json", suite.ID, string(b))
	return err
}

func (s *Store) SaveEnvironment(ctx context.Context, e domain.Environment) (domain.Environment, error) {
	if strings.TrimSpace(e.Name) == "" {
		return e, fmt.Errorf("environment name is required")
	}
	if e.Type != "local" && e.Type != "wsl" && e.Type != "ssh" {
		return e, fmt.Errorf("environment type must be local, wsl, or ssh")
	}
	if e.Type == "wsl" && e.Distribution == "" {
		return e, fmt.Errorf("a WSL distribution is required")
	}
	if e.Type == "ssh" {
		if e.Host == "" || strings.HasPrefix(e.Host, "-") || strings.ContainsAny(e.Host, "\r\n\t ;|&`$\\") {
			return e, fmt.Errorf("invalid SSH hostname or configured host alias")
		}
		if e.User != "" && !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(e.User) {
			return e, fmt.Errorf("invalid SSH user")
		}
		if e.Port < 1 || e.Port > 65535 {
			return e, fmt.Errorf("invalid SSH port")
		}
	}
	if strings.ContainsAny(e.Distribution+e.WorkDir+e.IdentityFile+e.KnownHostsFile+e.CACertPath, "\x00\r\n") {
		return e, fmt.Errorf("configuration paths must not contain control characters")
	}
	if e.ID == "" {
		e.ID = domain.NewID("env")
		e.CreatedAt = domain.Now()
	} else {
		old, err := s.Environment(ctx, e.ID)
		if err != nil {
			return e, err
		}
		e.CreatedAt = old.CreatedAt
	}
	if e.Tools == nil {
		e.Tools = map[string]string{}
	}
	if e.DockerGatewayMode != "" && e.DockerGatewayMode != "detect" && e.DockerGatewayMode != "manual" {
		return e, fmt.Errorf("invalid Docker gateway mode")
	}
	if e.DockerGatewayMode == "manual" && strings.TrimSpace(e.DockerHostGateway) == "" {
		return e, fmt.Errorf("manual Docker gateway requires an explicit address")
	}
	if e.Networks != nil {
		for _, id := range []string{e.Networks.SourceProfileID, e.Networks.BuildProfileID, e.Networks.AgentProfileID, e.Networks.VerifierProfileID} {
			if id == "" {
				continue
			}
			profile, err := s.NetworkProfile(ctx, id)
			if err != nil {
				return e, err
			}
			if profile.Scope != domain.NetworkEnvironment || profile.EnvironmentID != e.ID {
				return e, fmt.Errorf("network profile must target this execution host")
			}
		}
	}
	e.NetworkRuntime = nil
	b, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO environments(id,name,type,config_json,created_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,config_json=excluded.config_json`, e.ID, e.Name, e.Type, string(b), e.CreatedAt)
	return e, err
}

func (s *Store) Environment(ctx context.Context, id string) (domain.Environment, error) {
	var b string
	var e domain.Environment
	err := s.DB.GetContext(ctx, &b, "SELECT config_json FROM environments WHERE id=?", id)
	if err == sql.ErrNoRows {
		return e, domain.ErrNotFound
	}
	if err != nil {
		return e, err
	}
	err = json.Unmarshal([]byte(b), &e)
	return e, err
}
func (s *Store) Environments(ctx context.Context) ([]domain.Environment, error) {
	var raw []string
	if err := s.DB.SelectContext(ctx, &raw, "SELECT config_json FROM environments ORDER BY created_at,id"); err != nil {
		return nil, err
	}
	items := make([]domain.Environment, 0, len(raw))
	for _, b := range raw {
		var e domain.Environment
		if err := json.Unmarshal([]byte(b), &e); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, nil
}

type providerRow struct {
	ID               string         `db:"id"`
	Name             string         `db:"name"`
	BaseURL          string         `db:"base_url"`
	CredentialID     sql.NullString `db:"credential_id"`
	CreatedAt        string         `db:"created_at"`
	APIKey           sql.NullString `db:"api_key"`
	NetworkProfileID string         `db:"network_profile_id"`
}

func (r providerRow) public() domain.Provider {
	p := domain.Provider{ID: r.ID, Name: r.Name, BaseURL: r.BaseURL, CreatedAt: r.CreatedAt, HasKey: r.APIKey.Valid && r.APIKey.String != "", NetworkProfileID: r.NetworkProfileID}
	if p.HasKey {
		p.KeyHint = "••••"
		p.KeyRevision = r.CredentialID.String
	}
	return p
}

const providerSelect = `SELECT p.id,p.name,p.base_url,p.credential_id,p.created_at,p.network_profile_id,c.api_key FROM providers p LEFT JOIN provider_credentials c ON c.id=p.credential_id`

func (s *Store) SaveProvider(ctx context.Context, in domain.ProviderInput) (domain.Provider, error) {
	var p domain.Provider
	if strings.TrimSpace(in.Name) == "" {
		return p, fmt.Errorf("provider name is required")
	}
	u, err := url.Parse(in.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return p, fmt.Errorf("provider base URL must be an HTTP(S) endpoint without credentials, query or fragment")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return p, fmt.Errorf("unencrypted HTTP is only permitted for loopback providers")
	}
	if in.APIKey != nil && (len(*in.APIKey) > 16384 || strings.ContainsAny(*in.APIKey, "\r\n\x00")) {
		return p, fmt.Errorf("invalid API key")
	}
	var credentialID, networkProfileID string
	created := domain.Now()
	id := in.ID
	if id == "" {
		id = domain.NewID("provider")
	} else {
		old, err := s.Provider(ctx, id)
		if err != nil {
			return p, err
		}
		created = old.CreatedAt
		credentialID = old.KeyRevision
		networkProfileID = old.NetworkProfileID
	}
	if in.NetworkProfileID != nil {
		networkProfileID = *in.NetworkProfileID
		if networkProfileID != "" {
			profile, err := s.NetworkProfile(ctx, networkProfileID)
			if err != nil {
				return p, err
			}
			if profile.Scope != domain.NetworkDesktop {
				return p, fmt.Errorf("provider network must target the Desktop host")
			}
		}
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO providers(id,name,base_url,credential_id,created_at,network_profile_id) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,base_url=excluded.base_url,network_profile_id=excluded.network_profile_id`, id, in.Name, strings.TrimRight(in.BaseURL, "/"), nil, created, networkProfileID)
	if err != nil {
		return p, err
	}
	if in.APIKey != nil {
		credentialID = ""
		if *in.APIKey == "" {
			if _, err = tx.ExecContext(ctx, "UPDATE provider_credentials SET api_key='' WHERE provider_id=?", id); err != nil {
				return p, err
			}
		}
		if *in.APIKey != "" {
			credentialID = domain.NewID("credential")
			_, err = tx.ExecContext(ctx, "INSERT INTO provider_credentials(id,provider_id,api_key,created_at) VALUES(?,?,?,?)", credentialID, id, *in.APIKey, domain.Now())
			if err != nil {
				return p, err
			}
		}
		var cid any
		if credentialID != "" {
			cid = credentialID
		}
		if _, err = tx.ExecContext(ctx, "UPDATE providers SET credential_id=? WHERE id=?", cid, id); err != nil {
			return p, err
		}
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	return s.Provider(ctx, id)
}
func (s *Store) Provider(ctx context.Context, id string) (domain.Provider, error) {
	var r providerRow
	err := s.DB.GetContext(ctx, &r, providerSelect+" WHERE p.id=?", id)
	if err == sql.ErrNoRows {
		return domain.Provider{}, domain.ErrNotFound
	}
	return r.public(), err
}
func (s *Store) Providers(ctx context.Context) ([]domain.Provider, error) {
	var rows []providerRow
	if err := s.DB.SelectContext(ctx, &rows, providerSelect+" ORDER BY p.created_at,p.id"); err != nil {
		return nil, err
	}
	out := make([]domain.Provider, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.public())
	}
	return out, nil
}
func (s *Store) Credential(ctx context.Context, providerID, revision string) (string, error) {
	var key string
	if revision == "" {
		return "", fmt.Errorf("provider has no API key")
	}
	err := s.DB.GetContext(ctx, &key, "SELECT api_key FROM provider_credentials WHERE id=? AND provider_id=? AND api_key<>''", revision, providerID)
	if err == sql.ErrNoRows {
		return "", domain.ErrNotFound
	}
	return key, err
}

var decimalPrice = regexp.MustCompile(`^(0|[1-9][0-9]{0,15})(\.[0-9]{1,12})?$`)

func (s *Store) SaveModel(ctx context.Context, m domain.Model) (domain.Model, error) {
	if m.Name == "" || strings.ContainsAny(m.Name, "\x00\r\n") || !m.Protocol.Valid() {
		return m, fmt.Errorf("model name and a supported native protocol are required")
	}
	if m.DisplayName == "" {
		m.DisplayName = m.Name
	}
	if _, err := s.Provider(ctx, m.ProviderID); err != nil {
		return m, err
	}
	if p := m.Pricing; p != nil {
		if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(p.Currency) || p.Source == "" || !decimalPrice.MatchString(p.InputPerMillion) || !decimalPrice.MatchString(p.OutputPerMillion) {
			return m, fmt.Errorf("pricing requires a three-letter currency, source and non-negative decimal per-million rates")
		}
		for _, v := range []*string{p.CacheReadPerMillion, p.CacheWritePerMillion} {
			if v != nil && !decimalPrice.MatchString(*v) {
				return m, fmt.Errorf("cache prices must be non-negative decimals")
			}
		}
	}
	if m.ID == "" {
		m.ID = domain.NewID("model")
		m.CreatedAt = domain.Now()
	} else {
		old, err := s.Model(ctx, m.ID)
		if err != nil {
			return m, err
		}
		if old.ProviderID != m.ProviderID {
			return m, fmt.Errorf("an existing model cannot be moved to a different provider")
		}
		m.CreatedAt = old.CreatedAt
	}
	b, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO models(id,provider_id,name,protocol,config_json,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,protocol=excluded.protocol,config_json=excluded.config_json`, m.ID, m.ProviderID, m.Name, m.Protocol, string(b), m.CreatedAt)
	return m, err
}
func (s *Store) Model(ctx context.Context, id string) (domain.Model, error) {
	var raw string
	var m domain.Model
	err := s.DB.GetContext(ctx, &raw, "SELECT config_json FROM models WHERE id=?", id)
	if err == sql.ErrNoRows {
		return m, domain.ErrNotFound
	}
	if err != nil {
		return m, err
	}
	err = json.Unmarshal([]byte(raw), &m)
	return m, err
}
func (s *Store) Models(ctx context.Context) ([]domain.Model, error) {
	var raws []string
	if err := s.DB.SelectContext(ctx, &raws, "SELECT config_json FROM models ORDER BY created_at,id"); err != nil {
		return nil, err
	}
	out := make([]domain.Model, 0, len(raws))
	for _, raw := range raws {
		var m domain.Model
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
func (s *Store) Suites(ctx context.Context) ([]domain.Suite, error) {
	var raws []string
	if err := s.DB.SelectContext(ctx, &raws, "SELECT config_json FROM evaluation_suites ORDER BY id"); err != nil {
		return nil, err
	}
	out := make([]domain.Suite, 0, len(raws))
	for _, raw := range raws {
		var suite domain.Suite
		if err := json.Unmarshal([]byte(raw), &suite); err != nil {
			return nil, err
		}
		out = append(out, suite)
	}
	return out, nil
}
