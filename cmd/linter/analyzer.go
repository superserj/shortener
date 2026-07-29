package main

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// panicBuiltin — имя встроенной функции, аварийно завершающей выполнение.
	panicBuiltin = "panic"
	// mainPkg и mainFunc задают единственное место, где допустим выход из процесса.
	mainPkg  = "main"
	mainFunc = "main"
	// osPkgPath и osExitFunc описывают os.Exit — прямой выход в обход defer.
	osPkgPath  = "os"
	osExitFunc = "Exit"
	// logPkgPath и logFatalPrefix описывают семейство log.Fatal/Fatalf/Fatalln:
	// все они логируют сообщение и вызывают os.Exit(1).
	logPkgPath     = "log"
	logFatalPrefix = "Fatal"
)

// ExitCheckAnalyzer ищет в коде аварийные завершения программы: использование
// встроенной функции panic, а также вызовы log.Fatal и os.Exit за пределами
// функции main пакета main.
//
// Такие вызовы обрывают процесс в обход отложенных функций: не закрываются
// соединения с базой, не сбрасываются буферы логов, не отрабатывает graceful
// shutdown. Единственная точка выхода из программы — main, остальной код должен
// возвращать ошибку вызывающей стороне.
//
// Функциональный литерал считается самостоятельной функцией, поэтому os.Exit
// внутри горутины, запущенной из main, тоже попадёт в отчёт.
//
// Анализатор работает по дереву разбора и сопоставляет вызовы с объектами из
// вывода типов, поэтому видит вызов и через точечный импорт, и в скобках. Вызов
// через промежуточную переменную (exit := os.Exit; exit(1)) он не отследит: для
// этого нужен анализ потока данных на SSA, что для проверки единственной точки
// выхода из программы избыточно.
var ExitCheckAnalyzer = &analysis.Analyzer{
	Name: "exitcheck",
	Doc:  "сообщает об использовании panic и о вызовах log.Fatal и os.Exit вне функции main пакета main",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			decl, ok := n.(*ast.FuncDecl)
			if !ok {
				// вызовы вне тела функции — например, в инициализаторах
				// переменных уровня пакета — из main тоже не сделаны
				if call, ok := n.(*ast.CallExpr); ok {
					checkCall(pass, call, false)
				}
				return true
			}
			inspectBody(pass, decl.Body, isMainFunc(pass, decl))
			return false
		})
	}
	return nil, nil
}

// isMainFunc сообщает, объявлена ли decl как функция main пакета main.
func isMainFunc(pass *analysis.Pass, decl *ast.FuncDecl) bool {
	return pass.Pkg.Name() == mainPkg && decl.Recv == nil && decl.Name.Name == mainFunc
}

// inspectBody обходит тело функции, помня, разрешён ли в нём выход из процесса.
func inspectBody(pass *analysis.Pass, body ast.Node, inMainFunc bool) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			inspectBody(pass, node.Body, false)
			return false
		case *ast.CallExpr:
			checkCall(pass, node, inMainFunc)
		}
		return true
	})
}

func checkCall(pass *analysis.Pass, call *ast.CallExpr, inMainFunc bool) {
	// вызываемое имя может быть завёрнуто в скобки: (os.Exit)(1)
	var ident *ast.Ident
	switch fun := unparen(call.Fun).(type) {
	case *ast.Ident:
		// точечный импорт даёт os.Exit и log.Fatal без квалификатора,
		// поэтому одиночный идентификатор проверяется наравне с селектором
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return
	}

	if isBuiltinPanic(pass, ident) {
		pass.Reportf(call.Pos(), "использование встроенной функции panic")
		return
	}
	if inMainFunc {
		return
	}

	pkgPath, name, ok := qualifiedFunc(pass, ident)
	if !ok {
		return
	}
	switch {
	case pkgPath == osPkgPath && name == osExitFunc:
		pass.Reportf(call.Pos(), "вызов os.Exit вне функции main пакета main")
	case pkgPath == logPkgPath && strings.HasPrefix(name, logFatalPrefix):
		pass.Reportf(call.Pos(), "вызов log.%s вне функции main пакета main", name)
	}
}

// unparen снимает скобки вокруг выражения: (os.Exit)(1) вызывает ту же функцию,
// что и os.Exit(1).
func unparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

// isBuiltinPanic отличает встроенный panic от одноимённой функции, объявленной
// в самом пакете: имени в исходниках недостаточно, нужен вывод типов.
func isBuiltinPanic(pass *analysis.Pass, ident *ast.Ident) bool {
	if ident.Name != panicBuiltin {
		return false
	}
	_, ok := pass.TypesInfo.Uses[ident].(*types.Builtin)
	return ok
}

// qualifiedFunc возвращает путь пакета и имя вызываемой функции. Методы
// пропускаются: (*log.Logger).Fatal завершает процесс так же, но требование
// инкремента сформулировано про функции пакета log.
func qualifiedFunc(pass *analysis.Pass, ident *ast.Ident) (pkgPath, name string, ok bool) {
	fn, ok := pass.TypesInfo.Uses[ident].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return "", "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil {
		return "", "", false
	}
	return fn.Pkg().Path(), fn.Name(), true
}
