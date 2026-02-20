package portal

//easyjson:json
type propertyStatsPoint struct {
	Date  int64 `json:"x"`
	Value int   `json:"y"`
}

//easyjson:json
type propertyStatsResponse struct {
	Requested []*propertyStatsPoint `json:"requested"`
	Verified  []*propertyStatsPoint `json:"verified"`
}

//easyjson:json
type accountStatsPoint struct {
	Date   int64 `json:"x"`
	Value  int   `json:"y"`
	Series int   `json:"s"`
}

//easyjson:json
type accountStatsRawPoint struct {
	OrgID int32
	Date  int64
	Value int
}

//easyjson:json
type accountStatsSeries struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

//easyjson:json
type accountStatsResponse struct {
	Series []*accountStatsSeries `json:"series"`
	Data   []*accountStatsPoint  `json:"data"`
}

//easyjson:json
type propertyRuleStatsPoint struct {
	Date  int64 `json:"x"`
	Value int   `json:"y"`
}

//easyjson:json
type propertyRuleStatsResponse struct {
	Usage []*propertyRuleStatsPoint `json:"usage"`
}
