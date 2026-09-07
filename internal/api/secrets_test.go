package api

import (
	"strings"
	"testing"
)

func TestParseCSVSecrets_Chrome(t *testing.T) {
	csvData := `name,url,username,password,note
Google,https://accounts.google.com,alice@gmail.com,secret123,my primary email
GitHub,https://github.com/login,alice_dev,gh_pass456,
`
	parsed, err := parseCSVSecrets(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error parsing Chrome CSV: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed secrets, got %d", len(parsed))
	}

	if parsed[0].Title != "Google" || parsed[0].Username != "alice@gmail.com" || parsed[0].Value != "secret123" {
		t.Errorf("unexpected first item: %+v", parsed[0])
	}
	if parsed[1].Title != "GitHub" || parsed[1].Username != "alice_dev" || parsed[1].Value != "gh_pass456" {
		t.Errorf("unexpected second item: %+v", parsed[1])
	}
}

func TestParseCSVSecrets_Firefox(t *testing.T) {
	csvData := `"url","username","password","httpRealm","formActionOrigin","guid","timeCreated","timeLastUsed","timePasswordChanged"
"https://gitlab.com","bob_gitlab","gl_pass789","","https://gitlab.com","{123}","1600000000","1600000000","1600000000"
`
	parsed, err := parseCSVSecrets(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error parsing Firefox CSV: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 parsed secret, got %d", len(parsed))
	}

	if parsed[0].Title != "Gitlab.com" || parsed[0].Username != "bob_gitlab" || parsed[0].Value != "gl_pass789" {
		t.Errorf("unexpected parsed item: %+v", parsed[0])
	}
}

func TestParseCSVSecrets_Safari(t *testing.T) {
	csvData := `Title,URL,Username,Password,Notes,OTPAuth
Apple ID,https://appleid.apple.com,charlie@icloud.com,apple_pass321,,
`
	parsed, err := parseCSVSecrets(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error parsing Safari CSV: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 parsed secret, got %d", len(parsed))
	}

	if parsed[0].Title != "Apple ID" || parsed[0].Username != "charlie@icloud.com" || parsed[0].Value != "apple_pass321" {
		t.Errorf("unexpected parsed item: %+v", parsed[0])
	}
}

func TestParseCSVSecrets_Bitwarden(t *testing.T) {
	csvData := `folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp
,0,login,DigitalOcean,Dev VPS,,0,https://cloud.digitalocean.com,dave@example.com,do_pass654,
`
	parsed, err := parseCSVSecrets(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error parsing Bitwarden CSV: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 parsed secret, got %d", len(parsed))
	}

	if parsed[0].Title != "DigitalOcean" || parsed[0].Username != "dave@example.com" || parsed[0].Value != "do_pass654" {
		t.Errorf("unexpected parsed item: %+v", parsed[0])
	}
}
