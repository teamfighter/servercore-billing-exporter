#!/usr/bin/env bash
# Converts Servercore OpenStack RC files into config.ini for the exporter.
#
# Usage:
#   OS_PASSWORD=secret ./scripts/generate-config.sh rc-file1.sh rc-file2.sh ...
#   # Append to existing config.ini (for a second password group):
#   OS_PASSWORD=other  ./scripts/generate-config.sh --append rc-file3.sh rc-file4.sh ...

set -euo pipefail

APPEND=false
if [ "${1:-}" = "--append" ]; then
    APPEND=true
    shift
fi

if [ $# -eq 0 ]; then
    echo "Usage: $0 [--append] <rc-file1.sh> [rc-file2.sh] ..."
    echo "Set OS_PASSWORD env var or the script will prompt for it."
    echo "Use --append to add to existing config.ini without overwriting."
    exit 1
fi

# Get password from env or prompt once.
if [ -z "${OS_PASSWORD:-}" ]; then
    echo -n "OpenStack password (used for all projects in this batch): "
    read -rs OS_PASSWORD
    echo
fi

OUTPUT="config.ini"
if [ "$APPEND" = false ]; then
    : > "$OUTPUT"
fi

for rc_file in "$@"; do
    if [ ! -f "$rc_file" ]; then
        echo "ERROR: File not found: $rc_file" >&2
        exit 1
    fi

    # Extract section name from filename (strip path, extension, common prefixes).
    section=$(basename "$rc_file" .sh)
    section=${section#rc-}
    section=${section#openrc-}

    # Parse exports from the RC file.
    auth_url=$(grep -oP 'OS_AUTH_URL=["'"'"']?\K[^"'"'"']+' "$rc_file" || true)
    project_id=$(grep -oP 'OS_PROJECT_ID=["'"'"']?\K[^"'"'"']+' "$rc_file" || true)
    domain_name=$(grep -oP 'OS_PROJECT_DOMAIN_NAME=["'"'"']?\K[^"'"'"']+' "$rc_file" || true)
    region_name=$(grep -oP 'OS_REGION_NAME=["'"'"']?\K[^"'"'"']+' "$rc_file" || true)
    username=$(grep -oP 'OS_USERNAME=["'"'"']?\K[^"'"'"']+' "$rc_file" || true)

    cat >> "$OUTPUT" <<EOF
[$section]
auth_url = $auth_url
project_id = $project_id
domain_name = $domain_name
region_name = $region_name
username = $username
password = $OS_PASSWORD

EOF

    echo "  ✓ [$section] (project_id=${project_id:0:8}...)"
done

echo ""
if [ "$APPEND" = true ]; then
    echo "Appended $# project(s) to $OUTPUT."
else
    echo "Generated $OUTPUT with $# project(s)."
fi
echo "⚠  Keep this file secure — it contains credentials."
