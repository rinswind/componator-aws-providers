#!/bin/bash
# Build and push deployment-operator container to minikube and AWS ECR
set -e

# Change to repository root (script is in scripts/ subdirectory)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$REPO_ROOT"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}→ $1${NC}"
}

log_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

log_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Build image once
build_image() {
    local img=$1
    log_info "Building image: $img"
    make docker-build IMG="$img"
    log_success "Image built"
}

# Tag image with additional tags
tag_image() {
    local source=$1
    local target=$2
    log_info "Tagging $source → $target"
    docker tag "$source" "$target"
}

# Load all tags to minikube
load_to_minikube() {
    local prefix=$1
    shift
    local tags=("$@")
    
    echo ""
    log_info "Loading images to minikube..."
    for tag in "${tags[@]}"; do
        local img="${prefix}:${tag}"
        minikube image load "$img"
        log_success "Loaded: $img"
    done
}

# Push all tags to ECR
push_to_ecr() {
    local prefix=$1
    local aws_account_id=$2
    local aws_region=$3
    shift 3
    local tags=("$@")
    
    echo ""
    log_info "Logging into AWS ECR..."
    aws ecr get-login-password --region "$aws_region" | \
        docker login --username AWS --password-stdin "${aws_account_id}.dkr.ecr.${aws_region}.amazonaws.com"
    log_success "Logged into ECR"
    
    echo ""
    log_info "Pushing images to ECR..."
    for tag in "${tags[@]}"; do
        local img="${prefix}:${tag}"
        make docker-push IMG="$img"
        log_success "Pushed: $img"
    done
}

# Get AWS account ID and region
log_info "Getting AWS account details..."
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
AWS_REGION=$(aws configure get region)

if [ -z "$AWS_ACCOUNT_ID" ]; then
    log_error "Could not determine AWS account ID"
    exit 1
fi

if [ -z "$AWS_REGION" ]; then
    log_warning "No default region configured, using us-east-1"
    AWS_REGION="us-east-1"
fi

log_success "AWS Account: $AWS_ACCOUNT_ID"
log_success "AWS Region: $AWS_REGION"

# Image configuration
IMG_NAME="deployment-operator-handlers"
LATEST_GIT_TAG=$(git tag --list | sort -V | tail -1)

# Build list of tags: latest + git tag (if exists)
IMG_TAGS=("latest")
if [ -n "$LATEST_GIT_TAG" ]; then
    IMG_TAGS+=("$LATEST_GIT_TAG")
fi

log_info "Image tags: ${IMG_TAGS[*]}"

# Image prefixes
MINIKUBE_PREFIX="rinswind/${IMG_NAME}"
ECR_PREFIX="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/rinswind/${IMG_NAME}"

# Phase 1: Build image once with "latest" tag
echo ""
build_image "${MINIKUBE_PREFIX}:latest"

# Phase 2: Create additional tags for minikube
for tag in "${IMG_TAGS[@]:1}"; do
    tag_image "${MINIKUBE_PREFIX}:latest" "${MINIKUBE_PREFIX}:${tag}"
done

# Phase 3: Load all tags to minikube
load_to_minikube "$MINIKUBE_PREFIX" "${IMG_TAGS[@]}"

# Phase 4: Tag for ECR
echo ""
log_info "Creating ECR tags..."
for tag in "${IMG_TAGS[@]}"; do
    tag_image "${MINIKUBE_PREFIX}:${tag}" "${ECR_PREFIX}:${tag}"
done
log_success "ECR tags created"

# Phase 5: Push all tags to ECR
push_to_ecr "$ECR_PREFIX" "$AWS_ACCOUNT_ID" "$AWS_REGION" "${IMG_TAGS[@]}"

echo ""
log_success "Complete!"
echo -e "  Minikube images:"
for tag in "${IMG_TAGS[@]}"; do
    echo -e "    - ${MINIKUBE_PREFIX}:${tag}"
done
echo -e "  ECR images:"
for tag in "${IMG_TAGS[@]}"; do
    echo -e "    - ${ECR_PREFIX}:${tag}"
done
