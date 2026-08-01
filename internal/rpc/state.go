package rpc

import (
	"wimy/internal/wm"
)

// State is the JSON representation of the model served by the state
// method and state notifications.
type State struct {
	Outputs []OutputState `json:"outputs"`
	Views   []ViewState   `json:"views"`
	Windows []WindowState `json:"windows"`
}

// OutputState describes one output.
type OutputState struct {
	Name    string  `json:"name"`
	View    string  `json:"view"`
	Focused bool    `json:"focused"`
	Rect    wm.Rect `json:"rect"`
}

// ViewState describes one view.
type ViewState struct {
	Name          string      `json:"name"`
	Output        string      `json:"output"` // output showing it, "" if none
	Mode          string      `json:"mode"`   // mode of the focused column
	Columns       int         `json:"columns"`
	Occupied      bool        `json:"occupied"`
	FocusedWindow wm.WindowID `json:"focused_window"`
}

// WindowState describes one window.
type WindowState struct {
	ID       wm.WindowID `json:"id"`
	AppID    string      `json:"app_id"`
	Title    string      `json:"title"`
	Tags     []string    `json:"tags"`
	Focused  bool        `json:"focused"`
	Floating bool        `json:"floating"`
	Rect     *wm.Rect    `json:"rect,omitempty"` // rendered geometry, if placed
}

// buildState converts the model to its JSON representation. It must be
// called under Backend.Snapshot.
func buildState(s *wm.State) State {
	st := State{
		Outputs: make([]OutputState, 0, len(s.Outputs)),
		Views:   make([]ViewState, 0, len(s.Views)),
		Windows: make([]WindowState, 0, len(s.Windows)),
	}
	shownOn := make(map[string]string) // view -> output name
	for i, o := range s.Outputs {
		shownOn[o.View] = o.Name
		st.Outputs = append(st.Outputs, OutputState{
			Name:    o.Name,
			View:    o.View,
			Focused: i == s.FocusOutput,
			Rect:    o.Rect,
		})
	}
	rects := layoutRects(s)
	for _, v := range s.Views {
		vs := ViewState{
			Name:          v.Name,
			Output:        shownOn[v.Name],
			Columns:       len(v.Columns),
			Occupied:      !viewEmpty(v),
			FocusedWindow: v.FocusedWindow(),
		}
		if v.FocusCol >= 0 && v.FocusCol < len(v.Columns) {
			vs.Mode = v.Columns[v.FocusCol].Mode.String()
		}
		st.Views = append(st.Views, vs)
	}
	for _, w := range s.Windows {
		st.Windows = append(st.Windows, WindowState{
			ID:       w.ID,
			AppID:    w.AppID,
			Title:    w.Title,
			Tags:     w.TagList(),
			Focused:  w.ID == s.Focused,
			Floating: floatsAnywhere(s, w.ID),
			Rect:     rects[w.ID],
		})
	}
	return st
}

func viewEmpty(v *wm.View) bool {
	for _, c := range v.Columns {
		if len(c.Windows) > 0 {
			return false
		}
	}
	return len(v.Float) == 0
}

func floatsAnywhere(s *wm.State, id wm.WindowID) bool {
	for _, v := range s.Views {
		for _, f := range v.Float {
			if f == id {
				return true
			}
		}
	}
	return false
}

// layoutRects maps window IDs to their rendered content rects.
func layoutRects(s *wm.State) map[wm.WindowID]*wm.Rect {
	out := make(map[wm.WindowID]*wm.Rect)
	for _, p := range s.Layout() {
		if p.Hidden {
			continue
		}
		r := p.Rect
		out[p.ID] = &r
	}
	return out
}
