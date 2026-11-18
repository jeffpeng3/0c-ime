package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/0c-biggo/0c-ime/internal/modelsets"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

// jaPackageModels loads the active Japanese model package from active.json.
// The package root is the directory 0c-model-set install wrote; each artifact
// lives in its own directory with manifest.json, vocab.json, the tokenizer and
// the selected ONNX weight.
func jaPackageModels(lockPath, root, dataDir string, backend neural.Backend,
	rerankThreads int, rerankTimeout time.Duration,
	punctThreads int, punctTimeout time.Duration, ortLibrary string) ([]neural.ModelSpec, *modelsets.Deployment, error) {
	if root == "" {
		return nil, nil, nil
	}
	selection, err := modelsets.ReadSelection(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return nil, nil, err
	}
	pkg := lock.ModelPackage
	if pkg == nil {
		return nil, nil, fmt.Errorf("模型包清單缺少 model_package")
	}
	if selection.Version != "" && selection.Version != pkg.Version {
		return nil, nil, fmt.Errorf("active.json 版本 %s 與模型包 %s 不符", selection.Version, pkg.Version)
	}
	if selection.Dictionary.ManifestSHA256 != "" && selection.Dictionary.ManifestSHA256 != pkg.Dictionary.ManifestSHA256 {
		return nil, nil, fmt.Errorf("active.json 綁定的字典與模型包不符")
	}
	installed, err := installedJADictionary(dataDir)
	if err != nil {
		return nil, nil, err
	}
	if installed.SHA256 != pkg.Dictionary.ManifestSHA256 {
		return nil, nil, fmt.Errorf("已安裝日文詞典 %s 與模型包 %s 不符", installed.SHA256, pkg.Dictionary.ManifestSHA256)
	}
	var specs []neural.ModelSpec
	for i := range pkg.Artifacts {
		artifact := &pkg.Artifacts[i]
		pkgRoot := artifact.PackageRoot(root)
		tokenizer := artifact.AssetPath(root, "tokenizer.model")
		switch artifact.Kind {
		case modelsets.KindReranker:
			specs = append(specs, neural.ModelSpec{
				Name: "ja-rerank", Language: "ja", Role: neural.RoleRerank,
				Backend: backend, ORTLibrary: ortLibrary, Package: pkgRoot,
				DictionarySHA256: pkg.Dictionary.ManifestSHA256, Tokenizer: tokenizer,
				Threads: rerankThreads, Timeout: rerankTimeout, Preload: true,
			})
		case modelsets.KindPunctuation:
			specs = append(specs, neural.ModelSpec{
				Name: "ja-punct", Language: "ja", Role: neural.RoleJAPunct,
				Backend: backend, ORTLibrary: ortLibrary, Package: pkgRoot,
				DictionarySHA256: pkg.Dictionary.ManifestSHA256, Tokenizer: tokenizer,
				Threads: punctThreads, Timeout: punctTimeout, Preload: true,
			})
		}
	}
	_ = selection
	return specs, pkg.Deployment, nil
}

func modelRootForPackage(flagValue, user string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.Join(filepath.Dir(user), "models")
}

func jaModelStatus(handle *neural.Handle) string {
	if handle == nil {
		return "not_installed"
	}
	if state := handle.State(); state != "" {
		return state
	}
	return "unknown"
}

func jaModelQuality(handle *neural.Handle) string {
	if handle == nil {
		return ""
	}
	return handle.Quality()
}

func jaModelPlaceholder(handle *neural.Handle) bool {
	return handle != nil && handle.Placeholder()
}

func jaModelReason(handle *neural.Handle) string {
	if handle == nil {
		return ""
	}
	return handle.Reason()
}
