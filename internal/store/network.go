package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func (s *Store) NetworkProfile(ctx context.Context, id string) (domain.NetworkProfile, error) {
	var raw string
	err := s.DB.GetContext(ctx, &raw, `SELECT v.config_json FROM network_profiles p JOIN network_profile_versions v ON v.revision=p.revision AND v.profile_id=p.id WHERE p.id=?`, id)
	if err == sql.ErrNoRows {
		return domain.NetworkProfile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.NetworkProfile{}, err
	}
	var profile domain.NetworkProfile
	err = json.Unmarshal([]byte(raw), &profile)
	return profile, err
}

func (s *Store) NetworkProfileRevision(ctx context.Context, id, revision string) (domain.NetworkProfile, error) {
	var raw string
	err := s.DB.GetContext(ctx, &raw, `SELECT config_json FROM network_profile_versions WHERE profile_id=? AND revision=?`, id, revision)
	if err == sql.ErrNoRows {
		return domain.NetworkProfile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.NetworkProfile{}, err
	}
	var profile domain.NetworkProfile
	err = json.Unmarshal([]byte(raw), &profile)
	return profile, err
}

func (s *Store) NetworkProfiles(ctx context.Context) ([]domain.NetworkProfile, error) {
	var rows []string
	if err := s.DB.SelectContext(ctx, &rows, `SELECT v.config_json FROM network_profiles p JOIN network_profile_versions v ON v.revision=p.revision AND v.profile_id=p.id ORDER BY p.created_at,p.id`); err != nil {
		return nil, err
	}
	profiles := make([]domain.NetworkProfile, 0, len(rows))
	for _, raw := range rows {
		var profile domain.NetworkProfile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *Store) ProxyCredential(ctx context.Context, profileID, revision string) (domain.ProxyCredential, error) {
	var row struct {
		Username string `db:"username"`
		Password string `db:"password"`
	}
	if revision == "" {
		return domain.ProxyCredential{}, errors.New("approved proxy credential reference is unavailable")
	}
	err := s.DB.GetContext(ctx, &row, `SELECT username,password FROM proxy_credentials WHERE id=? AND profile_id=? AND username<>'' AND password<>''`, revision, profileID)
	if err == sql.ErrNoRows {
		return domain.ProxyCredential{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ProxyCredential{}, err
	}
	credential := domain.ProxyCredential{Username: row.Username, Password: row.Password}
	return credential, credential.Validate()
}

func (s *Store) SaveNetworkProfile(ctx context.Context, input domain.NetworkProfileInput) (domain.NetworkProfile, error) {
	profile := input.Profile
	profile.TLSRiskAccepted = profile.TLSMode == domain.NetworkSkipTLS && input.ConfirmTLSRisk
	if err := profile.Validate(); err != nil {
		return domain.NetworkProfile{}, err
	}
	if profile.Scope == domain.NetworkEnvironment {
		if _, err := s.Environment(ctx, profile.EnvironmentID); err != nil {
			return domain.NetworkProfile{}, fmt.Errorf("selected execution host is unavailable: %w", err)
		}
	}
	var old domain.NetworkProfile
	var err error
	creating := profile.ID == ""
	if creating {
		profile.ID, profile.CreatedAt = domain.NewID("network"), domain.Now()
	} else {
		old, err = s.NetworkProfile(ctx, profile.ID)
		if err != nil {
			return domain.NetworkProfile{}, err
		}
		if profile.Revision == "" || profile.Revision != old.Revision {
			return domain.NetworkProfile{}, domain.ErrConflict
		}
		profile.CreatedAt = old.CreatedAt
	}
	var credential domain.ProxyCredential
	credentialRevision := old.CredentialRevision
	if input.ClearCredentials {
		credentialRevision = ""
	}
	writingCredential := input.ProxyUsername != nil || input.ProxyPassword != nil
	if writingCredential {
		if profile.ProxyMode != domain.NetworkProxy || profile.ProxyAuth != domain.NetworkBasicAuth {
			return domain.NetworkProfile{}, errors.New("proxy credential writes require explicitly selected Basic proxy authentication")
		}
		if credentialRevision != "" && (input.ProxyUsername == nil || input.ProxyPassword == nil) {
			credential, err = s.ProxyCredential(ctx, profile.ID, credentialRevision)
			if err != nil {
				return domain.NetworkProfile{}, errors.New("proxy credential replacement requires both fields")
			}
		}
		if input.ProxyUsername != nil {
			credential.Username = *input.ProxyUsername
		}
		if input.ProxyPassword != nil {
			credential.Password = *input.ProxyPassword
		}
		if err = credential.Validate(); err != nil {
			return domain.NetworkProfile{}, err
		}
		credentialRevision = domain.NewID("proxycredential")
	}
	profile.HasCredentials, profile.CredentialRevision = credentialRevision != "", credentialRevision
	if profile.ProxyAuth == domain.NetworkBasicAuth && !writingCredential {
		if !profile.HasCredentials {
			return domain.NetworkProfile{}, errors.New("authenticated proxy credentials are required")
		}
		if _, err := s.ProxyCredential(ctx, profile.ID, credentialRevision); err != nil {
			return domain.NetworkProfile{}, errors.New("retained proxy credential reference is unavailable")
		}
	}
	profile.Revision = domain.NewID("networkrevision")
	if err = profile.ValidateSaved(); err != nil {
		return domain.NetworkProfile{}, err
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return domain.NetworkProfile{}, err
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return domain.NetworkProfile{}, err
	}
	defer tx.Rollback()
	var environmentID any
	if profile.EnvironmentID != "" {
		environmentID = profile.EnvironmentID
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO network_profiles(id,name,scope,environment_id,revision,created_at) VALUES(?,?,?,?,?,?)`, profile.ID, profile.Name, profile.Scope, environmentID, profile.Revision, profile.CreatedAt)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE network_profiles SET name=?,scope=?,environment_id=?,revision=? WHERE id=? AND revision=?`, profile.Name, profile.Scope, environmentID, profile.Revision, profile.ID, old.Revision)
		if err == nil {
			affected, countErr := result.RowsAffected()
			if countErr != nil {
				err = countErr
			} else if affected != 1 {
				err = domain.ErrConflict
			}
		}
	}
	if err != nil {
		return domain.NetworkProfile{}, err
	}
	if input.ClearCredentials {
		if _, err = tx.ExecContext(ctx, `UPDATE proxy_credentials SET username='',password='' WHERE profile_id=?`, profile.ID); err != nil {
			return domain.NetworkProfile{}, err
		}
	}
	if writingCredential {
		if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_credentials(id,profile_id,username,password,created_at) VALUES(?,?,?,?,?)`, credentialRevision, profile.ID, credential.Username, credential.Password, domain.Now()); err != nil {
			return domain.NetworkProfile{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO network_profile_versions(revision,profile_id,config_json) VALUES(?,?,?)`, profile.Revision, profile.ID, string(raw)); err != nil {
		return domain.NetworkProfile{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.NetworkProfile{}, err
	}
	return profile, nil
}

func (s *Store) FreezeNetwork(ctx context.Context, environment domain.Environment, provider domain.Provider) (domain.NetworkSnapshot, error) {
	if environment.Networks == nil || !environment.Networks.Complete() || provider.NetworkProfileID == "" {
		return domain.NetworkSnapshot{}, errors.New("select source, build, agent, verifier and model network profiles explicitly")
	}
	cache := map[string]domain.NetworkProfile{}
	load := func(id string) (domain.NetworkProfile, error) {
		if profile, ok := cache[id]; ok {
			return profile, nil
		}
		profile, err := s.NetworkProfile(ctx, id)
		if err == nil {
			cache[id] = profile
		}
		return profile, err
	}
	var snapshot domain.NetworkSnapshot
	var err error
	bindings := []struct {
		id          string
		destination *domain.NetworkProfile
	}{
		{environment.Networks.SourceProfileID, &snapshot.Source}, {environment.Networks.BuildProfileID, &snapshot.Build},
		{environment.Networks.AgentProfileID, &snapshot.Agent}, {environment.Networks.VerifierProfileID, &snapshot.Verifier}, {provider.NetworkProfileID, &snapshot.Model},
	}
	for _, binding := range bindings {
		*binding.destination, err = load(binding.id)
		if err != nil {
			return domain.NetworkSnapshot{}, err
		}
	}
	return snapshot, snapshot.Validate(environment.ID)
}

func (s *Store) ExecutionNetworks(ctx context.Context, snapshot domain.NetworkSnapshot) (*domain.ExecutionNetworks, error) {
	networks := &domain.ExecutionNetworks{Snapshot: snapshot, Credentials: map[string]domain.ProxyCredential{}}
	for _, profile := range snapshot.ExecutionProfiles() {
		if profile.ProxyAuth != domain.NetworkBasicAuth {
			continue
		}
		credential, err := s.ProxyCredential(ctx, profile.ID, profile.CredentialRevision)
		if err != nil {
			return nil, errors.New("approved execution proxy credential reference is unavailable")
		}
		networks.Credentials[profile.ID+"@"+profile.CredentialRevision] = credential
	}
	return networks, nil
}

func (s *Store) SourceEnvironment(ctx context.Context, environment domain.Environment) (domain.Environment, error) {
	if environment.Networks == nil || environment.Networks.SourceProfileID == "" {
		return environment, errors.New("select a source network profile before checking or downloading from this host")
	}
	profile, err := s.NetworkProfile(ctx, environment.Networks.SourceProfileID)
	if err != nil {
		return environment, err
	}
	if err = profile.ValidateSaved(); err != nil {
		return environment, err
	}
	if profile.Scope != domain.NetworkEnvironment || profile.EnvironmentID != environment.ID {
		return environment, errors.New("source network does not target this execution host")
	}
	networks := &domain.ExecutionNetworks{Snapshot: domain.NetworkSnapshot{Source: profile}, Credentials: map[string]domain.ProxyCredential{}}
	if profile.ProxyAuth == domain.NetworkBasicAuth {
		credential, err := s.ProxyCredential(ctx, profile.ID, profile.CredentialRevision)
		if err != nil {
			return environment, errors.New("source proxy credential reference is unavailable")
		}
		networks.Credentials[profile.ID+"@"+profile.CredentialRevision] = credential
	}
	environment.ProxyURL, environment.NoProxy, environment.CACertPath, environment.SkipTLSVerification = "", "", "", false
	environment.NetworkRuntime = networks
	return environment, nil
}
