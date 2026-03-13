#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────
# Clarion 质量门禁
#
# 用法:
#   scripts/quality-gate.sh                    # 完整检查
#   scripts/quality-gate.sh --baseline         # 记录基线快照
#   scripts/quality-gate.sh --check            # 对比基线检查（覆盖率不能降）
#   scripts/quality-gate.sh --pkg internal/... # 只检查指定包
#
# 检查项:
#   1. 编译通过
#   2. go vet
#   3. golangci-lint（严格模式）
#   4. 全量测试通过（-race）
#   5. 覆盖率不低于基线
#   6. 源文件无 TODO/FIXME（可选）
#
# 退出码: 0=通过, 1=失败
# ─────────────────────────────────────────────────────────────────
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

BASELINE_FILE="$PROJECT_ROOT/scripts/.quality-baseline"
COV_PROFILE="/tmp/clarion_quality_gate.out"
PKG="${QG_PKG:-./internal/...}"

# ── 颜色 ─────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_step()  { echo -e "\n${BOLD}[$1/${TOTAL_STEPS}]${NC} $2"; }
log_ok()    { echo -e "  ${GREEN}✓${NC} $*"; }
log_fail()  { echo -e "  ${RED}✗${NC} $*"; }
log_warn()  { echo -e "  ${YELLOW}⚠${NC} $*"; }
log_info()  { echo -e "  ${CYAN}→${NC} $*"; }

TOTAL_STEPS=5
FAILURES=0
MODE="full"

# ── 参数解析 ──────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --baseline)  MODE="baseline"; shift ;;
        --check)     MODE="check"; shift ;;
        --pkg)       PKG="$2"; shift 2 ;;
        -h|--help)
            echo "用法: scripts/quality-gate.sh [--baseline|--check] [--pkg PKG]"
            exit 0
            ;;
        *) echo "未知参数: $1" >&2; exit 1 ;;
    esac
done

# ── 获取覆盖率数值 ───────────────────────────────────────────
get_coverage_pct() {
    go test -short -coverprofile="$COV_PROFILE" $PKG 2>/dev/null | tail -1
    go tool cover -func="$COV_PROFILE" 2>/dev/null | grep "^total:" | awk '{print $NF}' | tr -d '%'
}

# ── 基线模式 ──────────────────────────────────────────────────
if [[ "$MODE" == "baseline" ]]; then
    echo -e "${BOLD}记录质量基线...${NC}"
    COV=$(get_coverage_pct)
    TOTAL_TESTS=$(go test -short -count=1 $PKG 2>&1 | grep -c "^ok" || true)
    cat > "$BASELINE_FILE" <<EOF
coverage=$COV
total_tests=$TOTAL_TESTS
timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
pkg=$PKG
EOF
    echo -e "${GREEN}基线已记录:${NC} 覆盖率=${COV}% 测试数=${TOTAL_TESTS}"
    exit 0
fi

# ── 检查开始 ──────────────────────────────────────────────────
echo -e "${BOLD}═══════════════════════════════════════${NC}"
echo -e "${BOLD}  Clarion 质量门禁${NC}"
echo -e "${BOLD}═══════════════════════════════════════${NC}"
echo -e "  包: $PKG"
echo -e "  模式: $MODE"

START_TIME=$(date +%s)

# ── Step 1: 编译 ──────────────────────────────────────────────
log_step 1 "编译检查"
if go build $PKG 2>/tmp/clarion_qg_build.log; then
    log_ok "编译通过"
else
    log_fail "编译失败"
    cat /tmp/clarion_qg_build.log
    FAILURES=$((FAILURES + 1))
fi

# ── Step 2: go vet ────────────────────────────────────────────
log_step 2 "go vet"
if go vet $PKG 2>/tmp/clarion_qg_vet.log; then
    log_ok "vet 通过"
else
    log_fail "vet 发现问题"
    cat /tmp/clarion_qg_vet.log
    FAILURES=$((FAILURES + 1))
fi

# ── Step 3: golangci-lint ─────────────────────────────────────
log_step 3 "golangci-lint"
if golangci-lint run $PKG 2>/tmp/clarion_qg_lint.log; then
    log_ok "lint 通过"
else
    LINT_ERRORS=$(wc -l < /tmp/clarion_qg_lint.log)
    log_fail "lint 发现 ${LINT_ERRORS} 个问题"
    head -30 /tmp/clarion_qg_lint.log
    if [[ $LINT_ERRORS -gt 30 ]]; then
        log_warn "（仅显示前 30 条，完整输出见 /tmp/clarion_qg_lint.log）"
    fi
    FAILURES=$((FAILURES + 1))
fi

# ── Step 4: 测试 ──────────────────────────────────────────────
log_step 4 "测试（-race）"
if go test -race -count=1 -short $PKG 2>/tmp/clarion_qg_test.log 1>/tmp/clarion_qg_test.log; then
    TEST_COUNT=$(grep -c "^ok" /tmp/clarion_qg_test.log || true)
    log_ok "全部通过（${TEST_COUNT} 个包）"
else
    log_fail "测试失败"
    # 只显示失败的部分
    grep -A 5 "FAIL" /tmp/clarion_qg_test.log | head -30
    FAILURES=$((FAILURES + 1))
fi

# ── Step 5: 覆盖率 ───────────────────────────────────────────
log_step 5 "覆盖率检查"
CURRENT_COV=$(get_coverage_pct)
log_info "当前覆盖率: ${CURRENT_COV}%"

if [[ "$MODE" == "check" && -f "$BASELINE_FILE" ]]; then
    BASELINE_COV=$(grep "^coverage=" "$BASELINE_FILE" | cut -d= -f2)
    if [[ -n "$BASELINE_COV" ]]; then
        # 用 awk 做浮点比较
        if awk "BEGIN {exit !($CURRENT_COV < $BASELINE_COV - 0.5)}"; then
            log_fail "覆盖率下降: ${BASELINE_COV}% → ${CURRENT_COV}%（不允许降低超过 0.5%）"
            FAILURES=$((FAILURES + 1))
        else
            log_ok "覆盖率未下降: ${BASELINE_COV}% → ${CURRENT_COV}%"
        fi
    fi
elif [[ "$MODE" == "check" ]]; then
    log_warn "未找到基线文件，跳过覆盖率对比（先运行 --baseline）"
fi

# ── 结果 ──────────────────────────────────────────────────────
END_TIME=$(date +%s)
ELAPSED=$(( END_TIME - START_TIME ))

echo ""
echo -e "${BOLD}───────────────────────────────────────${NC}"
if [[ $FAILURES -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}  质量门禁通过 ✓${NC}  (${ELAPSED}秒)"
else
    echo -e "${RED}${BOLD}  质量门禁失败 ✗${NC}  ${FAILURES} 项未通过  (${ELAPSED}秒)"
fi
echo -e "${BOLD}───────────────────────────────────────${NC}"

exit $FAILURES
