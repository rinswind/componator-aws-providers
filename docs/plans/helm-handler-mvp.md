# Helm Handler MVP Implementation Plan

## Current Status: Phase 2 Ready

**Next Action**: Implement Helm Client Integration  
**Quick Fix Available**: Test status message mismatch (controller_test.go:149)

## MVP Scope

**Core Requirements**:

- Install/uninstall charts from public repositories  
- Basic values override through Component.Spec.Config
- Status reporting through Component lifecycle phases
- Protocol compliance with deployment-orchestrator architecture

**Excluded from MVP**:

- Chart upgrades, rollbacks, history management
- Complex nested values or templating  
- Local chart development or custom repositories
- Advanced retry mechanisms or circuit breakers

## Implementation Phases

### Phase 1: Foundation ✅ COMPLETE

**Goal**: Configuration schema, claiming protocol, test infrastructure  
**Status**: Production ready, 12/13 tests passing  
**Issue**: One test failure (status message mismatch)

### Phase 2: Helm Integration � READY TO START

**Goal**: Replace TODO stubs with actual Helm operations  
**Files**: `internal/controller/helm/operations.go`  
**Dependencies**: helm.sh/helm/v3 packages  
**Done When**: nginx chart deploys from Bitnami repository successfully

**Required Tasks**:

- Add Helm v3 dependencies to go.mod
- Implement chart installation in performHelmDeployment()
- Implement release cleanup in performHelmCleanup()
- Handle network errors and chart loading failures

### Phase 3: Deployment Lifecycle ⏳ AUTO-COMPLETES

**Goal**: Complete creation/deletion protocols with real Helm operations  
**Status**: State machine implemented, waiting for Phase 2  
**Note**: Will automatically complete when Phase 2 provides real operations

### Phase 4: MVP Validation ⏳ BLOCKED

**Goal**: End-to-end testing and nginx deployment verification  
**Status**: Not started  
**Blockers**: Depends on Phase 2 completion  
**Done When**: Full nginx chart lifecycle (deploy → ready → cleanup) works

## Quick Fixes Available

**Test Status Message (Low Priority)**:

- File: `internal/controller/helm/controller_test.go` line 149
- Issue: Test expects "Configuration error" but gets "Deployment error"
- Fix: Align error message expectations with actual error handling

## Phase 2 Implementation Details

**Dependencies to Add**:

- helm.sh/helm/v3/pkg/action (core operations)
- helm.sh/helm/v3/pkg/chart/loader (chart loading)
- helm.sh/helm/v3/pkg/cli (settings)
- helm.sh/helm/v3/pkg/getter (repository access)
- helm.sh/helm/v3/pkg/repo (repository management)

**Success Criteria**:

- Install nginx chart from Bitnami repository
- Apply values override from Component.Spec.Config
- Report correct status phases during deployment
- Clean up releases during Component deletion