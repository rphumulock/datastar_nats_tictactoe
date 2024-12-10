// Code generated by templ - DO NOT EDIT.

// templ: version: v0.2.793
package components

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

import datastar "github.com/starfederation/datastar/code/go/sdk"

// "fmt"

// type TodoViewMode int

// const (
// 	TodoViewModeAll TodoViewMode = iota
// 	TodoViewModeActive
// 	TodoViewModeCompleted
// 	TodoViewModeLast
// )

// var TodoViewModeStrings = []string{"All", "Active", "Completed"}

// type Todo struct {
// 	Text      string `json:"text"`
// 	Completed bool   `json:"completed"`
// }

// type TodoMVC struct {
// 	Todos      []*Todo      `json:"todos"`
// 	EditingIdx int          `json:"editingIdx"`
// 	Mode       TodoViewMode `json:"mode"`
// }

// ------------------------------------------
type GameState struct {
	Board   [9]string // 9-cell board initialized with empty strings
	XIsNext bool      // Indicates if X is the next player
	Winner  string    // The winner of the game ("X" or "O")
}

//	templ TodosMVCView(mvc *GameState) {
//		<div id="todos-container" class="h-full relative border border-solid border-primary rounded p-2 my-2 mx-28">
//			<div
//				class="flex flex-col w-full gap-4"
//				data-store={ fmt.Sprintf("{input:'%s'}", input) }
//			>
//				<section class="flex flex-col gap-2">
//					<header class="flex flex-col gap-2">
//						<div class="flex items-baseline gap-2 justify-center">
//							<h1 class="text-4xl font-bold uppercase font-brand md:text-6xl text-primary">todos</h1>
//						</div>
//						<div class="flex items-center gap-2">
//							if hasTodos {
//								<div class="tooltip" data-tip="toggle all todos">
//									<button
//										id="toggleAll"
//										class="btn btn-lg"
//										data-on-click="$post('/api/todos/-1/toggle')"
//										data-indicator="toggleAllFetching"
//										data-bind-disabled="$toggleAllFetching"
//									>
//										@icon("material-symbols:checklist")
//									</button>
//								</div>
//							}
//							if mvc.EditingIdx <0 {
//								@TodoInput(-1)
//							}
//							@sseIndicator("toggleAllFetching")
//						</div>
//					</header>
//					if hasTodos {
//						// <section class="max-h-[calc(100vh-400px)] overflow-scroll">
//						// 	<ul class="divide-y divide-primary">
//						// 		for i, todo := range mvc.Todos {
//						// 			@TodoRow(mvc.Mode, todo, i, i == mvc.EditingIdx)
//						// 		}
//						// 	</ul>
//						// </section>
//						// <footer class="flex flex-wrap items-center justify-between gap-2">
//						// 	<span class="todo-count">
//						// 		<strong>
//						// 			{ fmt.Sprint(left) }
//						// 			if (len(mvc.Todos) > 1) {
//						// 				items
//						// 			} else {
//						// 				item
//						// 			}
//						// 		</strong> left
//						// 	</span>
//						// 	<div class="join">
//						// 		for i := TodoViewModeAll; i < TodoViewModeLast; i++ {
//						// 			if i == mvc.Mode {
//						// 				<div class="btn btn-xs btn-primary join-item">{ TodoViewModeStrings[i] }</div>
//						// 			} else {
//						// 				<button
//						// 					class="btn btn-xs join-item"
//						// 					data-on-click={ fmt.Sprintf("$put('/api/todos/mode/%d')", i) }
//						// 				>
//						// 					{ TodoViewModeStrings[i] }
//						// 				</button>
//						// 			}
//						// 		}
//						// 	</div>
//						// 	<div class="join">
//						// 		if completed > 0 {
//						// 			<div class="tooltip" data-tip={ fmt.Sprintf("clear %d completed todos", completed) }>
//						// 				<button
//						// 					class="btn btn-error btn-xs join-item"
//						// 					data-on-click="$delete('/api/todos/-1')"
//						// 				>
//						// 					@icon("material-symbols:delete")
//						// 				</button>
//						// 			</div>
//						// 		}
//						// 		<div class="tooltip" data-tip="Reset list">
//						// 			<button
//						// 				class="btn btn-warning btn-xs join-item"
//						// 				data-on-click="$put('/api/todos/reset')"
//						// 			>
//						// 				@icon("material-symbols:delete-sweep")
//						// 			</button>
//						// 		</div>
//						// 	</div>
//						// </footer>
//						// 	<footer class="flex justify-center text-xs">
//						// 		<div>Click to edit, click away to cancel, press enter to save.</div>
//						// 	</footer>
//					}
//				</section>
//			</div>
//		</div>
//	}
func GameMVCView(mvc *GameState) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var1 == nil {
			templ_7745c5c3_Var1 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("<div id=\"game-container\" class=\"h-full relative border border-solid border-primary rounded p-2 my-2 mx-28\"><div class=\"flex flex-col w-full h-full gap-4\"><section class=\"flex flex-col w-full h-full gap-2\"><header class=\"flex items-baseline gap-2 justify-center\"><h1 class=\"text-4xl font-bold uppercase font-brand md:text-6xl text-primary\">Multiplayer Tic-Tac-Toe</h1></header><!-- Tic Tac Toe Board --><div class=\"grid grid-cols-3 grid-rows-3 gap-2 w-full h-full mt-4\"><!-- Loop over the cells of the board -->")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		for i, cell := range mvc.Board {
			templ_7745c5c3_Err = Cell(cell, i).Render(ctx, templ_7745c5c3_Buffer)
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("</div></section><footer class=\"flex justify-center text-xs\"><button class=\"btn btn-primary text-lg\" data-on-click=\"")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var2 string
		templ_7745c5c3_Var2, templ_7745c5c3_Err = templ.JoinStringErrs(datastar.PostSSE("/api/game/reset"))
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: `web/components/game.templ`, Line: 149, Col: 56}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var2))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("\">Reset</button></footer></div></div>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return templ_7745c5c3_Err
	})
}

//	templ TodoInput(i int) {
//		<input
//			id="todoInput"
//			class="flex-1 w-full input input-bordered input-lg"
//			placeholder="What needs to be done?"
//			data-model="input"
//			data-on-keypress={ fmt.Sprintf(`
//				if (event.key === 'Enter' && $input.trim().length) {
//					$put('/api/todos/%d/edit');
//					$input = '';
//				}
//			`, i) }
//			if i >= 0 {
//				data-on-click.outside.capture="$put('/api/todos/cancel')"
//			}
//		/>
//	}
func Cell(cell string, i int) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var3 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var3 == nil {
			templ_7745c5c3_Var3 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)

		clicked := cell != ""
		var templ_7745c5c3_Var4 = []any{
			"w-full h-full border-primary flex items-center justify-center text-5xl font-semibold cursor-pointer",
			map[string]bool{
				"hover:border-gray-100 border-2 ": !clicked,
				"border-secondary border-8":       clicked,
			},
		}
		templ_7745c5c3_Err = templ.RenderCSSItems(ctx, templ_7745c5c3_Buffer, templ_7745c5c3_Var4...)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("<button class=\"")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var5 string
		templ_7745c5c3_Var5, templ_7745c5c3_Err = templ.JoinStringErrs(templ.CSSClasses(templ_7745c5c3_Var4).String())
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: `web/components/game.templ`, Line: 1, Col: 0}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var5))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("\" data-on-click=\"")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var6 string
		templ_7745c5c3_Var6, templ_7745c5c3_Err = templ.JoinStringErrs(datastar.PostSSE("/api/game/%d/toggle", i))
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: `web/components/game.templ`, Line: 187, Col: 60}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var6))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("\"")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		if cell != "" {
			_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(" disabled")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(">")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var7 string
		templ_7745c5c3_Var7, templ_7745c5c3_Err = templ.JoinStringErrs(cell)
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: `web/components/game.templ`, Line: 192, Col: 8}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var7))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("</button>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return templ_7745c5c3_Err
	})
}

//	{{
//		indicatorID := fmt.Sprintf("indicator%d", i)
//		fetchingSignalName := fmt.Sprintf("fetching%d", i)
//	}}
//
//	if isEditing {
//		@TodoInput(i)
//	} else if (
//
//	mode == TodoViewModeAll) ||
//	(mode == TodoViewModeActive && !todo.Completed) ||
//	(mode == TodoViewModeCompleted && todo.Completed) {
//
// <li class="flex items-center gap-8 p-2 group" id={ fmt.Sprintf("todo%d", i) }>
//
//			<label
//				id={ fmt.Sprintf("toggle%d", i) }
//				class="text-4xl cursor-pointer"
//				data-on-click={ datastar.PostSSE("/api/todos/%d/toggle", i) }
//				data-indicator={ fetchingSignalName }
//			>
//				if todo.Completed {
//					@icon("material-symbols:check-box-outline")
//				} else {
//					@icon("material-symbols:check-box-outline-blank")
//				}
//			</label>
//			<label
//				id={ indicatorID }
//				class="flex-1 text-lg cursor-pointer select-none"
//				data-on-click={ datastar.GetSSE("/api/todos/%d/edit", i) }
//				data-indicator={ fetchingSignalName }
//			>
//				{ todo.Text }
//			</label>
//			@sseIndicator(fetchingSignalName)
//			<button
//				id={ fmt.Sprintf("delete%d", i) }
//				class="invisible btn btn-error group-hover:visible"
//				data-on-click={ datastar.DeleteSSE("/api/todos/%d", i) }
//				data-indicator={ fetchingSignalName }
//				data-disabled={ "$" + fetchingSignalName }
//			>
//				@icon("material-symbols:close")
//			</button>
//		</li>
//	}
var _ = templruntime.GeneratedTemplate
