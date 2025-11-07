#!/bin/bash
set -e

OPERATOR_NAME="componator-aws-providers"

AWS_ACCOUNT="350996934557"
AWS_REGION="us-east-1"
REGISTRY="${AWS_ACCOUNT}.dkr.ecr.${AWS_REGION}.amazonaws.com/rinswind"

GIT_SHA=$(git rev-parse --short HEAD)
TIMESTAMP=$(date +%s)
VERSION="v0.1.0-dev-${GIT_SHA}-${TIMESTAMP}"

echo "Logging into AWS ECR..."
aws ecr get-login-password --region ${AWS_REGION} | docker login --username AWS --password-stdin ${REGISTRY}

echo "=========================================="
echo "Building ${OPERATOR_NAME} version: ${VERSION}"
echo "=========================================="

# Build and push operator image
OPERATOR_IMG="${REGISTRY}/${OPERATOR_NAME}:${VERSION}"

echo "=========================================="
echo "Building operator image ${OPERATOR_IMG}"
echo "=========================================="

make docker-build docker-push IMG="${OPERATOR_IMG}"

# Build and push bundle
BUNDLE_IMG="${REGISTRY}/${OPERATOR_NAME}-bundle:${VERSION}"

echo "=========================================="
echo "Building bundle image ${BUNDLE_IMG}"
echo "=========================================="

make bundle-build bundle-push \
    OPERATOR_IMG="${OPERATOR_IMG}" \
    BUNDLE_IMG="${BUNDLE_IMG}" \
    VERSION="${VERSION}"

echo "=========================================="
echo " Updating catalog with ${BUNDLE_IMG}"
echo "=========================================="

cd ../componator-olm-catalog
./scripts/update-dev-catalog.sh ${OPERATOR_NAME} ${BUNDLE_IMG}
cd ${OLDPWD}
