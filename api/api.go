package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"

	"github.com/Masterminds/semver/v3"
)

func New() http.Handler {
	mux := http.NewServeMux()

	handleInvalidPath(mux)
	mux.HandleFunc("GET /package/{package}/{version}", packageHandler)

	return mux
}

type npmPackageMetaResponse struct {
	Versions map[string]npmPackageResponse `json:"versions"`
}

type npmPackageResponse struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

type NpmPackageVersion struct {
	Name         string                        `json:"name"`
	Version      string                        `json:"version"`
	Dependencies map[string]*NpmPackageVersion `json:"dependencies"`
}

func packageHandler(w http.ResponseWriter, r *http.Request) {

	pkgName := r.PathValue("package")
	pkgVersion := r.PathValue("version")

	rootPkg := &NpmPackageVersion{Name: pkgName, Dependencies: map[string]*NpmPackageVersion{}}
	dependencyMap := make(map[string]string)
	if err := resolveDependencies(rootPkg, pkgVersion, dependencyMap); err != nil {
		log.Println(err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	stringified, err := json.MarshalIndent(map[string]interface{}{
		"name":         rootPkg.Name,
		"version":      rootPkg.Version,
		"dependencies": dependencyMap,
	}, "", "  ")
	if err != nil {
		log.Println(err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stringified)
}

func resolveDependencies(pkg *NpmPackageVersion, versionConstraint string, dependencyMap map[string]string) error {
	pkgMeta, err := fetchPackageMeta(pkg.Name)
	if err != nil {
		return err
	}
	concreteVersion, err := highestCompatibleVersion(versionConstraint, pkgMeta)
	if err != nil {
		return err
	}
	pkg.Version = concreteVersion

	// Fetch package details
	npmPkg, err := fetchPackage(pkg.Name, pkg.Version)
	if err != nil {
		return err
	}

	for dependencyName, dependencyVersionConstraint := range npmPkg.Dependencies {
		if _, exists := dependencyMap[dependencyName]; !exists {
			dep := &NpmPackageVersion{Name: dependencyName, Dependencies: map[string]*NpmPackageVersion{}}
			pkg.Dependencies[dependencyName] = dep
			if err := resolveDependencies(dep, dependencyVersionConstraint, dependencyMap); err != nil {
				return err
			}
			// Add to dependencyMap if it's a transitive dependency
			dependencyMap[dependencyName] = dep.Version
		}
	}

	return nil
}

func highestCompatibleVersion(constraintStr string, versions *npmPackageMetaResponse) (string, error) {
	constraint, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return "", err
	}
	filtered := filterCompatibleVersions(constraint, versions)
	sort.Sort(filtered)
	if len(filtered) == 0 {
		return "", errors.New("no compatible versions found")
	}
	return filtered[len(filtered)-1].String(), nil
}

func filterCompatibleVersions(constraint *semver.Constraints, pkgMeta *npmPackageMetaResponse) semver.Collection {
	var compatible semver.Collection
	for version := range pkgMeta.Versions {
		semVer, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		if constraint.Check(semVer) {
			compatible = append(compatible, semVer)
		}
	}
	return compatible
}

func fetchPackage(name, version string) (*npmPackageResponse, error) {
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s/%s", name, version))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func fetchPackageMeta(p string) (*npmPackageMetaResponse, error) {

	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s", p))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageMetaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}

func handleInvalidPath(mux *http.ServeMux) {
	mux.HandleFunc("/", invalidPath)
	mux.HandleFunc("/package", invalidPath)
	mux.HandleFunc("/package/", invalidPath)
	mux.HandleFunc("/package/{package}", invalidPath)
}

func invalidPath(w http.ResponseWriter, r *http.Request) {

	log.Printf("invalid request path: %s\n", r.URL.Path)
	http.Error(w, fmt.Sprintf("Invalid request path. Expected format: /package/{name}/{version}, but got %s", r.URL.Path), http.StatusBadRequest)
}
