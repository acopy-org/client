package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/riz/acopy-client/internal/config"
)

type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func Register(serverURL string, creds Credentials) error {
	body, _ := json.Marshal(creds)
	resp, err := http.Post(serverURL+"/api/users/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("email already registered")
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration failed (status %d)", resp.StatusCode)
	}
	return nil
}

func Login(cfg *config.Config, creds Credentials) error {
	body, _ := json.Marshal(creds)
	resp, err := http.Post(cfg.ServerURL+"/api/users/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid email or password")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed (status %d)", resp.StatusCode)
	}

	var lr loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	cfg.Token = lr.Token
	return cfg.Save()
}
