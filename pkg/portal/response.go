package portal

//easyjson:json
type PropertyStatsPoint struct {
	Date  int64 `json:"x"`
	Value int   `json:"y"`
}

//easyjson:json
type PropertyStatsResponse struct {
	Requested []*PropertyStatsPoint `json:"requested"`
	Verified  []*PropertyStatsPoint `json:"verified"`
}

type FormStatsPoint = PropertyStatsPoint

//easyjson:json
type FormStatsResponse struct {
	Success []*FormStatsPoint `json:"success"`
	Failure []*FormStatsPoint `json:"failure"`
}

//easyjson:json
type AccountStatsPoint struct {
	Date   int64 `json:"x"`
	Value  int   `json:"y"`
	Series int   `json:"s"`
}

//easyjson:json
type AccountStatsRawPoint struct {
	OrgID int32
	Date  int64
	Value int
}

//easyjson:json
type AccountStatsSeries struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

//easyjson:json
type AccountStatsResponse struct {
	Series []*AccountStatsSeries `json:"series"`
	Data   []*AccountStatsPoint  `json:"data"`
}

//easyjson:json
type PropertyRuleStatsPoint struct {
	Date  int64 `json:"x"`
	Value int   `json:"y"`
}

//easyjson:json
type PropertyRuleStatsResponse struct {
	Usage []*PropertyRuleStatsPoint `json:"usage"`
}
