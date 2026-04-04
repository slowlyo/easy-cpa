package backend

import "testing"

// TestExpectedCoreAssetSuffixFor 验证平台与资产映射。
func TestExpectedCoreAssetSuffixFor(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{name: "windows-amd64", goos: "windows", goarch: "amd64", want: "_windows_amd64.zip"},
		{name: "windows-arm64", goos: "windows", goarch: "arm64", want: "_windows_arm64.zip"},
		{name: "linux-amd64", goos: "linux", goarch: "amd64", want: "_linux_amd64.tar.gz"},
		{name: "darwin-arm64", goos: "darwin", goarch: "arm64", want: "_darwin_arm64.tar.gz"},
	}

	for _, testCase := range tests {
		got, err := expectedCoreAssetSuffixFor(testCase.goos, testCase.goarch)
		if err != nil {
			t.Fatalf("%s returned error: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Fatalf("%s got %s want %s", testCase.name, got, testCase.want)
		}
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
