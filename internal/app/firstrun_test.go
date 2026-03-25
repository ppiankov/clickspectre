package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetAppConfigDir(t *testing.T) {
	cases := []struct {
		name string
		home string
		xdg  string
	}{
		{
			name: "home_only",
			home: t.TempDir(),
		},
		{
			name: "home_and_xdg",
			home: t.TempDir(),
			xdg:  filepath.Join(t.TempDir(), "xdg"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", tc.home)
			t.Setenv("XDG_CONFIG_HOME", tc.xdg)

			baseDir, err := os.UserConfigDir()
			if err != nil {
				t.Fatalf("UserConfigDir() error = %v", err)
			}

			got, err := GetAppConfigDir()
			if err != nil {
				t.Fatalf("GetAppConfigDir() error = %v", err)
			}

			want := filepath.Join(baseDir, appName)
			if got != want {
				t.Fatalf("GetAppConfigDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestIsFirstRun(t *testing.T) {
	cases := []struct {
		name       string
		prepare    func(t *testing.T, appConfigDir string)
		wantCalls  []bool
		wantMarker bool
	}{
		{
			name:       "creates_marker_on_first_call",
			wantCalls:  []bool{true, false},
			wantMarker: true,
		},
		{
			name: "returns_false_when_marker_exists",
			prepare: func(t *testing.T, appConfigDir string) {
				if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				markerPath := filepath.Join(appConfigDir, markerFileName)
				if err := os.WriteFile(markerPath, []byte("done"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
			wantCalls:  []bool{false},
			wantMarker: true,
		},
		{
			name: "returns_false_when_app_config_path_is_file",
			prepare: func(t *testing.T, appConfigDir string) {
				parentDir := filepath.Dir(appConfigDir)
				if err := os.MkdirAll(parentDir, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(appConfigDir, []byte("not-a-directory"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
			wantCalls:  []bool{false},
			wantMarker: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			xdgDir := filepath.Join(homeDir, "xdg")

			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CONFIG_HOME", xdgDir)

			appConfigDir, err := GetAppConfigDir()
			if err != nil {
				t.Fatalf("GetAppConfigDir() error = %v", err)
			}

			if tc.prepare != nil {
				tc.prepare(t, appConfigDir)
			}

			for idx, want := range tc.wantCalls {
				if got := IsFirstRun(); got != want {
					t.Fatalf("IsFirstRun() call %d = %v, want %v", idx+1, got, want)
				}
			}

			markerPath := filepath.Join(appConfigDir, markerFileName)
			_, err = os.Stat(markerPath)
			gotMarker := err == nil
			if gotMarker != tc.wantMarker {
				t.Fatalf("marker exists = %v, want %v (err=%v)", gotMarker, tc.wantMarker, err)
			}
		})
	}
}
