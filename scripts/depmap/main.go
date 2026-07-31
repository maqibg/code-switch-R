// 一次性分析工具:解析 services/*.go(非测试),统计跨文件符号引用,
// 按域分组输出跨域依赖边。用于 A4/A5 拆包前的类型归属图。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 文件名前缀 → 域
func domainOf(file string) string {
	base := strings.TrimSuffix(filepath.Base(file), ".go")
	switch {
	case strings.HasPrefix(base, "migrations"), base == "database", base == "database_dsn", base == "dbwrite":
		return "db"
	case strings.HasPrefix(base, "atomic_write"), base == "fileutils", base == "userhome",
		base == "env_file_edit", base == "config_backup", base == "applog",
		strings.HasPrefix(base, "cmd_"), base == "servicestore":
		return "infra"
	case strings.HasPrefix(base, "pi_"):
		return "pi"
	case base == "pricingservice":
		return "pricing"
	case strings.HasPrefix(base, "log"), base == "logdashboardbundle":
		return "logging"
	case strings.HasPrefix(base, "blacklist"), base == "settingsservice":
		return "blacklist"
	case strings.HasPrefix(base, "codex_"), strings.HasPrefix(base, "relay_"),
		strings.HasPrefix(base, "protocol_"), strings.HasPrefix(base, "upstream_"),
		base == "providerrelay", base == "body_filter", base == "gemini_provider_bridge":
		return "relay"
	case strings.HasPrefix(base, "provider"), base == "platform_registry", base == "removed_platform_cleanup":
		return "provider"
	case base == "claudesettings", base == "codexsettings", base == "reasonixsettings",
		strings.HasPrefix(base, "json_proxy"), base == "cliconfigservice", base == "proxystate",
		base == "geminiservice", base == "networkservice",
		base == "directapply_helpers":
		return "clisettings"
	default:
		return "app"
	}
}

type declInfo struct {
	file   string
	domain string
	kind   string // type/func/method/var/const
}

func main() {
	fset := token.NewFileSet()
	files, _ := filepath.Glob("services/*.go")
	decls := map[string]declInfo{} // 顶层标识符 → 声明位置
	asts := map[string]*ast.File{}

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse:", f, err)
			continue
		}
		asts[f] = af
		d := domainOf(f)
		for _, dec := range af.Decls {
			switch n := dec.(type) {
			case *ast.FuncDecl:
				if n.Recv == nil {
					decls[n.Name.Name] = declInfo{f, d, "func"}
				}
			case *ast.GenDecl:
				for _, spec := range n.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						decls[s.Name.Name] = declInfo{f, d, "type"}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							kind := "var"
							if n.Tok == token.CONST {
								kind = "const"
							}
							decls[name.Name] = declInfo{f, d, kind}
						}
					}
				}
			}
		}
	}

	// 跨域引用:edge[fromDomain][toDomain][symbol] = count
	edge := map[string]map[string]map[string]int{}
	for f, af := range asts {
		from := domainOf(f)
		ast.Inspect(af, func(n ast.Node) bool {
			// 跳过 selector 的右侧(x.Field 的 Field 不是顶层标识符)
			if sel, ok := n.(*ast.SelectorExpr); ok {
				ast.Inspect(sel.X, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok {
						record(edge, decls, from, id.Name)
					}
					return true
				})
				return false
			}
			if id, ok := n.(*ast.Ident); ok {
				record(edge, decls, from, id.Name)
			}
			return true
		})
	}

	domains := []string{}
	for d := range edge {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	for _, from := range domains {
		tos := []string{}
		for to := range edge[from] {
			if to != from {
				tos = append(tos, to)
			}
		}
		sort.Strings(tos)
		for _, to := range tos {
			syms := edge[from][to]
			total := 0
			list := []string{}
			for s, c := range syms {
				total += c
				list = append(list, s)
			}
			sort.Slice(list, func(i, j int) bool { return syms[list[i]] > syms[list[j]] })
			if len(list) > 12 {
				list = list[:12]
			}
			parts := []string{}
			for _, s := range list {
				parts = append(parts, fmt.Sprintf("%s(%s,%d)", s, decls[s].kind, syms[s]))
			}
			fmt.Printf("%s -> %s  [%d refs, %d syms]\n    %s\n", from, to, total, len(syms), strings.Join(parts, " "))
		}
	}
}

func record(edge map[string]map[string]map[string]int, decls map[string]declInfo, from, name string) {
	di, ok := decls[name]
	if !ok || di.domain == from {
		return
	}
	if edge[from] == nil {
		edge[from] = map[string]map[string]int{}
	}
	if edge[from][di.domain] == nil {
		edge[from][di.domain] = map[string]int{}
	}
	edge[from][di.domain][name]++
}
