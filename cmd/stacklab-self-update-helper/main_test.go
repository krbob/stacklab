package main

import (
	"reflect"
	"testing"
)

func TestAPTUpgradeArgsPinsRequestedVersion(t *testing.T) {
	t.Parallel()

	got := aptUpgradeArgs("stacklab", "2026.07.09~nightly")
	want := []string{
		"install",
		"-y",
		"--only-upgrade",
		"-o",
		"Dpkg::Options::=--force-confold",
		"--",
		"stacklab=2026.07.09~nightly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptUpgradeArgs() = %#v, want %#v", got, want)
	}
}

func TestAPTUpgradeArgsAllowsLatestWhenVersionMissing(t *testing.T) {
	t.Parallel()

	got := aptUpgradeArgs("stacklab", "")
	want := []string{
		"install",
		"-y",
		"--only-upgrade",
		"-o",
		"Dpkg::Options::=--force-confold",
		"--",
		"stacklab",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptUpgradeArgs() = %#v, want %#v", got, want)
	}
}
