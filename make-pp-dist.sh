#!/bin/bash

# Make a zip file to distributed Playpen

set -o errexit
set -o pipefail
set -o nounset
#set -o xtrace

TELEPROXY_VERSION=0.4.6

SRCDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DIST="${SRCDIR}/dist"
mkdir -p "${DIST}"
VERSION=$(git describe --tags --dirty | sed s/playpen-origin-//)

echo "Building Playpen ${VERSION} distribution in ${DIST}"

echo "Downloading Teleproxy ${TELEPROXY_VERSION}..."
BASEURL=https://s3.amazonaws.com/datawire-static-files/teleproxy/${TELEPROXY_VERSION}
WGET="wget --timestamping --force-directories --no-host-directories --cut-dirs=3 --quiet"
for GOOSE in linux darwin; do
    (cd "${DIST}" && ${WGET} "${BASEURL}/${GOOSE}/amd64/teleproxy")
    cp -f "${DIST}/${GOOSE}/amd64/teleproxy" "${DIST}/pp-teleproxy-${GOOSE}-amd64"
    chmod 755 "${DIST}/pp-teleproxy-${GOOSE}-amd64"
done

echo "Building Playpen daemon..."
BLDDIR=$(mktemp -d)
trap "rm -rf $BLDDIR" EXIT
(cd "${SRCDIR}" && python3 -Wignore setup.py build -b "${BLDDIR}" > /dev/null)
sed "s/dev-version/${VERSION}/" < "${SRCDIR}/playpend" > "${BLDDIR}/lib/__main__.py"
python3 -m zipapp -o "${DIST}/playpend" -p "/usr/bin/env python3" -c "${BLDDIR}/lib"

echo "Building Playpen client..."
sed "s/dev-version/${VERSION}/" < "${SRCDIR}/playpen" > "${DIST}/playpen"
chmod 755 "${DIST}/playpen"
cp "${SRCDIR}/pp-launch" "${DIST}/pp-launch"

echo "Zipping up the distribution..."
RESULT="${DIST}/playpen-${VERSION}.zip"
rm -f "${RESULT}"
(cd "${DIST}" && zip -q6 "playpen-${VERSION}.zip" playpen playpend pp-launch pp-teleproxy-*-amd64)

echo "Done."
wc -c "${RESULT}"
