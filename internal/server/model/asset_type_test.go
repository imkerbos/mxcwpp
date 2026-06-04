package model

import "testing"

func TestDeriveAssetType(t *testing.T) {
	cases := []struct {
		name          string
		scope         string
		sourceHandler string
		want          string
	}{
		// scope=embedded → 永远 app
		{"embedded go", "embedded", "go_buildinfo", AssetTypeApp},
		{"embedded jar", "embedded", "jar_scanner", AssetTypeApp},
		{"embedded binary", "embedded", "binary_probe", AssetTypeApp},

		// scope=container → 永远 container
		{"container sbom", "container", "container_sbom", AssetTypeContainer},
		{"container empty handler", "container", "", AssetTypeContainer},

		// scope=system + OS 包管理 → os
		{"system rpm", "system", "rpm", AssetTypeOS},
		{"system dpkg", "system", "dpkg", AssetTypeOS},
		{"system apk", "system", "apk", AssetTypeOS},

		// scope=system + 中间件类 handler → middleware
		{"system jar", "system", "jar_scanner", AssetTypeMiddleware},
		{"system binary_probe", "system", "binary_probe", AssetTypeMiddleware},
		{"system go_buildinfo on host", "system", "go_buildinfo", AssetTypeMiddleware},
		{"system python", "system", "python", AssetTypeMiddleware},

		// scope 空 + handler 空 → unknown
		{"both empty", "", "", AssetTypeUnknown},
		// scope=system + 未识别 handler → 兜底 OS
		{"system unknown handler", "system", "weirdo", AssetTypeOS},

		// 大小写 / 空格
		{"upper scope", "EMBEDDED", "GO_BUILDINFO", AssetTypeApp},
		{"trim", "  system  ", "  rpm  ", AssetTypeOS},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveAssetType(c.scope, c.sourceHandler)
			if got != c.want {
				t.Errorf("DeriveAssetType(%q,%q) = %q, want %q",
					c.scope, c.sourceHandler, got, c.want)
			}
		})
	}
}

func TestDeriveFixOwner(t *testing.T) {
	cases := []struct {
		name         string
		assetType    string
		vulnCategory string
		want         string
	}{
		// app → dev
		{"app any → dev", AssetTypeApp, VulnCategoryLanguageDep, FixOwnerDev},
		{"app kernel → dev (asset wins)", AssetTypeApp, VulnCategoryKernel, FixOwnerDev},

		// container/image → image_maintainer
		{"container → image_maintainer", AssetTypeContainer, VulnCategoryLanguageDep, FixOwnerImageMaintainer},
		{"image → image_maintainer", AssetTypeImage, VulnCategoryKernel, FixOwnerImageMaintainer},

		// OS + vuln_category 决定细分
		{"os db_service → dba", AssetTypeOS, VulnCategoryDBService, FixOwnerDBA},
		{"os web_service → sre", AssetTypeOS, VulnCategoryWebService, FixOwnerSRE},
		{"os container_runtime → sre", AssetTypeOS, VulnCategoryContainerRuntime, FixOwnerSRE},
		{"os virtualization → sre", AssetTypeOS, VulnCategoryVirtualization, FixOwnerSRE},
		{"os kernel → ops", AssetTypeOS, VulnCategoryKernel, FixOwnerOps},
		{"os shared_lib → ops", AssetTypeOS, VulnCategorySharedLib, FixOwnerOps},
		{"os cli → ops", AssetTypeOS, VulnCategoryCliTool, FixOwnerOps},

		// middleware
		{"middleware db → dba", AssetTypeMiddleware, VulnCategoryDBService, FixOwnerDBA},
		{"middleware web → sre", AssetTypeMiddleware, VulnCategoryWebService, FixOwnerSRE},
		{"middleware language_dep → dev", AssetTypeMiddleware, VulnCategoryLanguageDep, FixOwnerDev},
		{"middleware other → sre", AssetTypeMiddleware, VulnCategoryOther, FixOwnerSRE},

		// unknown
		{"unknown → unknown", AssetTypeUnknown, VulnCategoryOther, FixOwnerUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveFixOwner(c.assetType, c.vulnCategory)
			if got != c.want {
				t.Errorf("DeriveFixOwner(%q,%q) = %q, want %q",
					c.assetType, c.vulnCategory, got, c.want)
			}
		})
	}
}
