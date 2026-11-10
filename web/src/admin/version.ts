// The "new version" check of the footer. Python asked PyPI; the Go version
// asks the GitHub releases of the project's repository. The repository
// address comes from the boot data (internal/version RepoURL).

// "https://github.com/owner/repo" -> the GitHub API address of its latest
// release, or null for an address that is not a GitHub repository.
export function latestReleaseURL(repoURL: string): string | null {
  const match = /^https:\/\/github\.com\/([^/]+)\/([^/]+?)\/?$/.exec(repoURL);
  return match ? `https://api.github.com/repos/${match[1]}/${match[2]}/releases/latest` : null;
}

// The address of a release page, from the release's tag ("v2.1").
export function releasePageURL(repoURL: string, tag: string): string {
  return repoURL.replace(/\/+$/, '') + '/releases/tag/' + encodeURIComponent(tag);
}

// The address of the Ukrainian documentation of a version, a release asset.
export function documentationURL(repoURL: string, version: string): string {
  return repoURL.replace(/\/+$/, '') + `/releases/download/v${version}/webquiz-documentation-uk.pdf`;
}

// "v2.1" -> "2.1"
export function versionFromTag(tag: string): string {
  return tag.replace(/^v/, '');
}

function parseVersion(version: string): number[] {
  return version.split('.').map((part) => parseInt(part, 10) || 0);
}

// True when `latest` is a higher version than `current` ("2.10" > "2.9").
export function isNewer(latest: string, current: string): boolean {
  const latestParts = parseVersion(latest);
  const currentParts = parseVersion(current);
  const length = Math.max(latestParts.length, currentParts.length);
  for (let i = 0; i < length; i++) {
    const a = latestParts[i] || 0;
    const b = currentParts[i] || 0;
    if (a > b) return true;
    if (a < b) return false;
  }
  return false;
}
