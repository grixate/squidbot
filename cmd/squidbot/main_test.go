package main

import "testing"

func TestMigrateCmdReturnsRemovedError(t *testing.T) {
	cmd := migrateCmd("")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected migrate command to return an error")
	}
	if err.Error() != legacyMigrateRemovedErr {
		t.Fatalf("expected %q, got %q", legacyMigrateRemovedErr, err.Error())
	}
}

func TestMigrateCmdAcceptsLegacyFlagsAndReturnsRemovedError(t *testing.T) {
	cmd := migrateCmd("")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--from-legacy-home", "~/.nanobot", "--merge-config=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected migrate command to return an error")
	}
	if err.Error() != legacyMigrateRemovedErr {
		t.Fatalf("expected %q, got %q", legacyMigrateRemovedErr, err.Error())
	}
}
