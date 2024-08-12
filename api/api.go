package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"

	"github.com/Masterminds/semver/v3"
)

func New() http.Handler {
	mux := http.NewServeMux()

	handleInvalidPath(mux)
	mux.HandleFunc("GET /package/{package}/{version}", packageHandler)

	return mux
}

const (
	packageDoesNotExistMsg = "Package does not exist"
	internalServerErrorMsg = "Internal server error"
	invalidRequestPathMsg  = "Invalid request path. Expected format: /package/{name}/{version}, but got %s"
)

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

	if err := resolveDependencies(rootPkg, pkgVersion); err != nil {
		println(err.Error())
		w.WriteHeader(500)
		return
	}

	/* get unique dependencies
	dependencyMap := make(map[string]string)
	if err := resolveDependenciesUnique(rootPkg, pkgVersion, dependencyMap); err != nil {
		log.Println(err.Error() + " in request " + r.URL.Path)
		http.Error(w, err.Error()+" in request "+r.URL.Path, http.StatusInternalServerError)
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
	*/

	stringified, err := json.MarshalIndent(rootPkg, "", "  ")
	if err != nil {
		println(err.Error())
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(stringified); err != nil {
		log.Println("Error writing response:", err)
		http.Error(w, internalServerErrorMsg, http.StatusInternalServerError)
		return
	}
	log.Printf("Successfully handled request for package: %s, version: %s", rootPkg.Name, rootPkg.Version)
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

func resolveDependenciesAsync(pkg *NpmPackageVersion, versionConstraint string, dependencyMap map[string]string) error {
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

	var wg sync.WaitGroup
	errChan := make(chan error, len(npmPkg.Dependencies))
	depChan := make(chan *NpmPackageVersion, len(npmPkg.Dependencies))

	// Log when goroutines start
	log.Printf("Starting to resolve dependencies for package: %s, version: %s", pkg.Name, pkg.Version)

	for dependencyName, dependencyVersionConstraint := range npmPkg.Dependencies {
		wg.Add(1)
		go func(depName, depVersionConstraint string) {
			defer wg.Done()
			log.Printf("Fetching and resolving dependency: %s", depName)

			if _, exists := dependencyMap[depName]; !exists {
				dep := &NpmPackageVersion{Name: depName, Dependencies: map[string]*NpmPackageVersion{}}
				log.Printf("Resolving dependencies for %s", depName)
				if err := resolveDependenciesAsync(dep, depVersionConstraint, dependencyMap); err != nil {
					log.Printf("Error resolving dependency %s: %v", depName, err)
					errChan <- err
					return
				}
				dependencyMap[depName] = dep.Version
				depChan <- dep
				log.Printf("Successfully resolved dependency: %s, version: %s", dep.Name, dep.Version)
			} else {
				log.Printf("Dependency %s already resolved with version %s", depName, dependencyMap[depName])
			}
		}(dependencyName, dependencyVersionConstraint)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)
	close(depChan)

	// Check if there were any errors
	if len(errChan) > 0 {
		return <-errChan
	}

	// Collect results from depChan
	for dep := range depChan {
		pkg.Dependencies[dep.Name] = dep
		log.Printf("Added dependency %s to package %s", dep.Name, pkg.Name)
	}

	log.Printf("Finished resolving dependencies for package: %s, version: %s", pkg.Name, pkg.Version)
	return nil
}

func resolveDependencies(pkg *NpmPackageVersion, versionConstraint string) error {
	pkgMeta, err := fetchPackageMeta(pkg.Name)
	if err != nil {
		return err
	}
	concreteVersion, err := highestCompatibleVersion(versionConstraint, pkgMeta)
	if err != nil {
		return err
	}
	pkg.Version = concreteVersion

	npmPkg, err := fetchPackage(pkg.Name, pkg.Version)
	if err != nil {
		return err
	}
	for dependencyName, dependencyVersionConstraint := range npmPkg.Dependencies {
		dep := &NpmPackageVersion{Name: dependencyName, Dependencies: map[string]*NpmPackageVersion{}}
		pkg.Dependencies[dependencyName] = dep
		if err := resolveDependencies(dep, dependencyVersionConstraint); err != nil {
			return err
		}
	}
	return nil
}

func resolveDependenciesUnique(pkg *NpmPackageVersion, versionConstraint string, dependencyMap map[string]string) error {
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
			if err := resolveDependenciesUnique(dep, dependencyVersionConstraint, dependencyMap); err != nil {
				return err
			}
			// Add to dependencyMap if it's a transitive dependency
			dependencyMap[dependencyName] = dep.Version
		}
	}

	return nil
}
