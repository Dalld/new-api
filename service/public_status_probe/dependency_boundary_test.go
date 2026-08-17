package public_status_probe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const publicStatusProbeModulePrefix = "github.com/QuantumNous/new-api/"

var publicStatusProbeForbiddenImportRoots = []string{
	publicStatusProbeModulePrefix + "controller",
	publicStatusProbeModulePrefix + "middleware",
	publicStatusProbeModulePrefix + "pkg/billingexpr",
	publicStatusProbeModulePrefix + "relay",
	publicStatusProbeModulePrefix + "service",
	publicStatusProbeModulePrefix + "setting/billing_setting",
}

var publicStatusProbeForbiddenSymbols = map[string]struct{}{
	"CacheGetRandomSatisfiedChannel":     {},
	"CacheUpdateChannelStatus":           {},
	"DecreaseTokenQuota":                 {},
	"DecreaseUserQuota":                  {},
	"DeltaUpdateUserQuota":               {},
	"GetNextEnabledKey":                  {},
	"GetRandomSatisfiedChannel":          {},
	"IncreaseTokenQuota":                 {},
	"IncreaseUserQuota":                  {},
	"LogQuotaData":                       {},
	"PostAudioConsumeQuota":              {},
	"PostConsumeQuota":                   {},
	"PostConsumeUserSubscriptionDelta":   {},
	"PostTextConsumeQuota":               {},
	"PostWssConsumeQuota":                {},
	"PreConsumeBilling":                  {},
	"PreConsumeTokenQuota":               {},
	"PreConsumeUserSubscription":         {},
	"PreWssConsumeQuota":                 {},
	"RecordConsumeLog":                   {},
	"RecordLog":                          {},
	"RecordLogWithAdminInfo":             {},
	"RecordTaskBillingLog":               {},
	"RefundSubscriptionPreConsume":       {},
	"SettleBilling":                      {},
	"SetupContextForSelectedChannel":     {},
	"UpdateChannelStatus":                {},
	"UpdateChannelUsedQuota":             {},
	"UpdateResponseTime":                 {},
	"UpdateUserUsedQuotaAndRequestCount": {},
	"testChannel":                        {},
	"testChannelWithOptions":             {},
}

func TestPublicStatusProbeProductionDependencyBoundary(t *testing.T) {
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	productionFiles := 0

	err = filepath.WalkDir(workingDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		productionFiles++
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imported := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			for _, forbiddenRoot := range publicStatusProbeForbiddenImportRoots {
				if importPath == forbiddenRoot || strings.HasPrefix(importPath, forbiddenRoot+"/") {
					t.Errorf("%s imports forbidden routing or billing package %q", filepath.Base(path), importPath)
				}
			}
		}

		file, parseErr = parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.SelectorExpr:
				if _, forbidden := publicStatusProbeForbiddenSymbols[expression.Sel.Name]; forbidden {
					position := fileSet.Position(expression.Sel.Pos())
					t.Errorf("%s:%d references forbidden routing, billing, quota, or consume-log symbol %s", filepath.Base(path), position.Line, expression.Sel.Name)
				}
			case *ast.CallExpr:
				identifier, ok := expression.Fun.(*ast.Ident)
				if !ok {
					break
				}
				if _, forbidden := publicStatusProbeForbiddenSymbols[identifier.Name]; forbidden {
					position := fileSet.Position(identifier.Pos())
					t.Errorf("%s:%d calls forbidden routing, billing, quota, or consume-log symbol %s", filepath.Base(path), position.Line, identifier.Name)
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	assert.Positive(t, productionFiles, "dependency boundary must inspect production Go files")
}
