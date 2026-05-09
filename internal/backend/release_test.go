package backend

import "testing"

// TestExpectedCoreAssetSuffixesFor 验证平台与资产映射。
func TestExpectedCoreAssetSuffixesFor(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   []string
	}{
		{name: "windows-amd64", goos: "windows", goarch: "amd64", want: []string{"_windows_amd64.zip"}},
		{name: "windows-arm64", goos: "windows", goarch: "arm64", want: []string{"_windows_aarch64.zip", "_windows_arm64.zip"}},
		{name: "linux-amd64", goos: "linux", goarch: "amd64", want: []string{"_linux_amd64.tar.gz"}},
		{name: "darwin-arm64", goos: "darwin", goarch: "arm64", want: []string{"_darwin_aarch64.tar.gz", "_darwin_arm64.tar.gz"}},
	}

	for _, testCase := range tests {
		got, err := expectedCoreAssetSuffixesFor(testCase.goos, testCase.goarch)
		if err != nil {
			t.Fatalf("%s returned error: %v", testCase.name, err)
		}
		if len(got) != len(testCase.want) {
			t.Fatalf("%s got len %d want %d", testCase.name, len(got), len(testCase.want))
		}
		for index := range got {
			if got[index] != testCase.want[index] {
				t.Fatalf("%s got %v want %v", testCase.name, got, testCase.want)
			}
		}
	}
}

// TestPickCoreAsset 验证核心资产匹配兼容实际命名。
func TestPickCoreAsset(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "CLIProxyAPI_6.10.9_windows_aarch64.zip"},
		{Name: "CLIProxyAPI_6.10.9_windows_amd64.zip"},
	}

	matcher, err := expectedCoreAssetMatcherFor("windows", "arm64")
	if err != nil {
		t.Fatalf("expected matcher without error: %v", err)
	}

	matched := false
	for _, asset := range assets {
		if matcher(asset) {
			if asset.Name != "CLIProxyAPI_6.10.9_windows_aarch64.zip" {
				t.Fatalf("got %s want %s", asset.Name, "CLIProxyAPI_6.10.9_windows_aarch64.zip")
			}
			matched = true
			break
		}
	}

	if !matched {
		t.Fatalf("expected matcher to find windows arm64 asset")
	}
}

// TestPickCoreAssetFallbackName 验证 arm64 历史命名仍可匹配。
func TestPickCoreAssetFallbackName(t *testing.T) {
	matcher, err := expectedCoreAssetMatcherFor("linux", "arm64")
	if err != nil {
		t.Fatalf("expected matcher without error: %v", err)
	}

	if !matcher(githubReleaseAsset{Name: "CLIProxyAPI_6.10.9_linux_arm64.tar.gz"}) {
		t.Fatalf("expected matcher to accept legacy arm64 suffix")
	}
	if !matcher(githubReleaseAsset{Name: "CLIProxyAPI_6.10.9_linux_aarch64.tar.gz"}) {
		t.Fatalf("expected matcher to accept aarch64 suffix")
	}
	if matcher(githubReleaseAsset{Name: "CLIProxyAPI_6.10.9_linux_amd64.tar.gz"}) {
		t.Fatalf("expected matcher to reject amd64 suffix")
	}
}

// TestExpectedAppAssetSuffixFor 验证应用资产映射。
func TestExpectedAppAssetSuffixFor(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want string
	}{
		{name: "windows", goos: "windows", want: "-windows.zip"},
		{name: "linux", goos: "linux", want: "-linux.tar.gz"},
		{name: "darwin", goos: "darwin", want: "-macos.zip"},
	}

	for _, testCase := range tests {
		got, err := expectedAppAssetSuffixFor(testCase.goos)
		if err != nil {
			t.Fatalf("%s returned error: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Fatalf("%s got %s want %s", testCase.name, got, testCase.want)
		}
	}
}

// TestCompareReleaseTags 验证版本比较逻辑。
func TestCompareReleaseTags(t *testing.T) {
	if CompareReleaseTags("v6.9.13", "v6.9.14") >= 0 {
		t.Fatalf("expected older version to be smaller")
	}
	if CompareReleaseTags("v6.9.14", "v6.9.14") != 0 {
		t.Fatalf("expected equal versions to compare as zero")
	}
	if CompareReleaseTags("v6.10.0", "v6.9.99") <= 0 {
		t.Fatalf("expected newer version to be greater")
	}
}
