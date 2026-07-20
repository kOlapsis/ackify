// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"testing"
)

func TestContainsEmail_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name   string
		emails []string
		target string
		want   bool
	}{
		{
			name:   "exact match",
			emails: []string{"john.doe@mycompany.com"},
			target: "john.doe@mycompany.com",
			want:   true,
		},
		{
			name:   "target differs in case",
			emails: []string{"john.doe@mycompany.com"},
			target: "John.Doe@MyCompany.com",
			want:   true,
		},
		{
			name:   "stored value differs in case",
			emails: []string{"John.Doe@mycompany.com"},
			target: "john.doe@mycompany.com",
			want:   true,
		},
		{
			name:   "surrounding whitespace ignored",
			emails: []string{"  John.Doe@mycompany.com  "},
			target: "john.doe@mycompany.com",
			want:   true,
		},
		{
			name:   "no match",
			emails: []string{"jane@mycompany.com"},
			target: "john.doe@mycompany.com",
			want:   false,
		},
		{
			name:   "empty list",
			emails: nil,
			target: "john.doe@mycompany.com",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsEmail(tt.emails, tt.target); got != tt.want {
				t.Errorf("containsEmail(%v, %q) = %v, want %v", tt.emails, tt.target, got, tt.want)
			}
		})
	}
}
