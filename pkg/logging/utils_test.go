package logging

import (
	"testing"
)

func TestMaskPhone(t *testing.T) {
	type args struct {
		input string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Basic phone number with country code",
			args: args{input: "+467338878654"},
			want: "+46XXXXXXX654",
		},
		{
			name: "Phone number without country code",
			args: args{input: "07338878654"},
			want: "073XXXXX654",
		},
		{
			name: "Short phone number",
			args: args{input: "1234"},
			want: "1234", // Too short to mask, should return unchanged
		},
		{
			name: "Long phone number",
			args: args{input: "+468123456789"},
			want: "+46XXXXXXX789",
		},
		{
			name: "Multiple phone numbers in one string",
			args: args{input: "Contact +467338878654 or 07338878654"},
			want: "Contact +46XXXXXXX654 or 073XXXXX654",
		},
		{
			name: "Phone number with spaces",
			args: args{input: "+46 733 887 8654"},
			want: "+46 XXX XXX 8654", // Keeping the last 3 digits visible
		},
		{
			name: "Phone number with dashes",
			args: args{input: "+46-733-887-8654"},
			want: "+46-XXX-XXX-8654", // Masking correctly with dashes
		},
		{
			name: "Phone number with date-like pattern",
			args: args{input: "2020-01-01"},
			want: "2020-01-01", // Should not mask date-like patterns
		},
		{
			name: "Phone number with date-like pattern without dashes",
			args: args{input: "20200101"},
			want: "20200101", // Should not mask date-like patterns
		},
		{
			name: "Phone number with timestamp-like pattern ",
			args: args{input: "2019-09-08T10:32:21Z"},
			want: "2019-09-08T10:32:21Z", // Should not mask timestamp-like patterns
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskPhone(tt.args.input); got != tt.want {
				t.Errorf("MaskPhone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaskEmail(t *testing.T) {
	type args struct {
		input string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Basic email masking",
			args: args{input: "john.doe@example.com"},
			want: "jXXXXXXe@example.com",
		},
		{
			name: "Email with short local part",
			args: args{input: "jd@example.com"},
			want: "XX@example.com",
		},
		{
			name: "Email with long local part",
			args: args{input: "avery.long.email@example.com"},
			want: "aXXXXXXXXXXXXXXl@example.com",
		},
		{
			name: "Multiple emails in one string",
			args: args{input: "Contact john.doe@example.com and jane.smith@domain.org"},
			want: "Contact jXXXXXXe@example.com and jXXXXXXXXh@domain.org",
		},
		{
			name: "Email without dots in local part",
			args: args{input: "username@domain.com"},
			want: "uXXXXXXe@domain.com",
		},
		{
			name: "Complex email with numbers",
			args: args{input: "user1234@domain.net"},
			want: "uXXXXXX4@domain.net",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskEmail(tt.args.input); got != tt.want {
				t.Errorf("MaskEmail() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDateLike(t *testing.T) {
	type args struct {
		input string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Date-like pattern with dashes",
			args: args{input: "2020-01-01"},
			want: true,
		},
		{
			name: "Date-like pattern without dashes",
			args: args{input: "20200101"},
			want: true,
		},
		{
			name: "Timestamp-like pattern",
			args: args{input: "2019-09-08T10:32:21Z"},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDateLike(tt.args.input); got != tt.want {
				t.Errorf("isDateLike() = %v, want %v", got, tt.want)
			}
		})
	}
}
