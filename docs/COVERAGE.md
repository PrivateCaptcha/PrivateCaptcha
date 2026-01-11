# Code Coverage HTML Report

The CI pipeline generates an HTML code coverage report that shows annotated source code with coverage highlighting. This report merges both unit test and integration test coverage data.

## Viewing the Report

### From GitHub Actions

1. Go to the **Actions** tab in the repository
2. Select a successful CI workflow run
3. In the **Artifacts** section, download `coverage-html`
4. Extract the zip file and open `coverage.html` in a browser

### Generating Locally

After running tests with coverage:

```bash
# Run unit tests with coverage
make test-unit-cover

# Run integration tests with coverage (requires Docker)
make test-docker

# Generate HTML report
./docker/generate-coverage-html.sh
```

The report will be generated at `coverage_html/coverage.html`.

## Coverage Exclusions

The following paths are excluded from the coverage report (matching SonarQube configuration):

- `pkg/db/migrations/**` - Database migrations
- `pkg/db/queries/**` - SQL query files
- `widget/**` - Widget JavaScript code (covered by JS tests)
- `web/**` - Web frontend code (covered by JS tests)
- `pkg/db/generated/**` - Generated code (sqlc)
- `cmd/**` - CLI entry points
- `pkg/db/tests/**` - Test helpers
- `pkg/portal/tests/**` - Test helpers

## Deploying to GitHub Pages (Private)

To host the coverage report on GitHub Pages for private repository access:

### Option 1: Manual Upload (Recommended for Private Repos)

1. Download the `coverage-html` artifact from Actions
2. Create a separate private repository for hosting (e.g., `PrivateCaptcha/coverage-reports`)
3. Upload the HTML file to the repository
4. Enable GitHub Pages in repository settings (Settings > Pages > Source: main branch)
5. Access at `https://privatecaptcha.github.io/coverage-reports/`

### Option 2: Automated Deployment Workflow

Add a separate workflow file `.github/workflows/coverage-pages.yaml`:

```yaml
name: Deploy Coverage to Pages

on:
  workflow_run:
    workflows: ["CI"]
    types:
      - completed
    branches:
      - main

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: "pages"
  cancel-in-progress: false

jobs:
  deploy:
    if: ${{ github.event.workflow_run.conclusion == 'success' }}
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - name: Download coverage artifact
        uses: actions/download-artifact@v4
        with:
          name: coverage-html
          path: coverage_html
          run-id: ${{ github.event.workflow_run.id }}
          github-token: ${{ secrets.GITHUB_TOKEN }}

      - name: Setup Pages
        uses: actions/configure-pages@v5

      - name: Upload artifact
        uses: actions/upload-pages-artifact@v3
        with:
          path: coverage_html

      - name: Deploy to GitHub Pages
        id: deployment
        uses: actions/deploy-pages@v4
```

**Note:** GitHub Pages for private repositories requires GitHub Pro, Team, or Enterprise plan.

### Access Control

For private repositories:
- GitHub Pages will require authentication
- Team members with repository access can view the coverage report
- Consider using branch protection to ensure only passing builds are deployed
