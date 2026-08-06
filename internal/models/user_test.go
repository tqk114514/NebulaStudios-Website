package models

import (
	"database/sql"
	"testing"
	"time"
)

func TestCheckBanned(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		user *User
		want bool
	}{
		{"nil user", nil, false},
		{"not banned", &User{IsBanned: false}, false},
		{"banned permanent", &User{IsBanned: true}, true},
		{"banned with future unban", &User{IsBanned: true, UnbanAt: sql.NullTime{Valid: true, Time: now.Add(time.Hour)}}, true},
		{"banned with past unban", &User{IsBanned: true, UnbanAt: sql.NullTime{Valid: true, Time: now.Add(-time.Hour)}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CheckBanned(); got != tt.want {
				t.Errorf("CheckBanned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPermanentBan(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		user *User
		want bool
	}{
		{"nil user", nil, false},
		{"not banned", &User{IsBanned: false}, false},
		{"banned no unban time", &User{IsBanned: true}, true},
		{"banned with unban time", &User{IsBanned: true, UnbanAt: sql.NullTime{Valid: true, Time: now.Add(time.Hour)}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.IsPermanentBan(); got != tt.want {
				t.Errorf("IsPermanentBan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserValidate(t *testing.T) {
	tests := []struct {
		name    string
		user    *User
		wantErr bool
	}{
		{"nil user", nil, true},
		{"empty username", &User{Email: "a@b.com", Password: "x"}, true},
		{"empty email", &User{Username: "alice", Password: "x"}, true},
		{"empty password", &User{Username: "alice", Email: "a@b.com"}, true},
		{"valid", &User{Username: "alice", Email: "a@b.com", Password: "x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.user.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
