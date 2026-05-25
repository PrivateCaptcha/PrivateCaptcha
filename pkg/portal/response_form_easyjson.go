package portal

import "github.com/mailru/easyjson/jwriter"

func (v FormStatsResponse) MarshalEasyJSON(w *jwriter.Writer) {
	w.RawByte('{')
	w.RawString("\"success\":")
	writeFormStatsPoints(w, v.Success)
	w.RawString(",\"failure\":")
	writeFormStatsPoints(w, v.Failure)
	w.RawByte('}')
}

func writeFormStatsPoints(w *jwriter.Writer, points []*FormStatsPoint) {
	if points == nil && (w.Flags&jwriter.NilSliceAsEmpty) == 0 {
		w.RawString("null")
		return
	}

	w.RawByte('[')
	for i, point := range points {
		if i > 0 {
			w.RawByte(',')
		}
		if point == nil {
			w.RawString("null")
			continue
		}
		point.MarshalEasyJSON(w)
	}
	w.RawByte(']')
}
