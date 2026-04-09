package tui

func (model *rootModel) apiTabRow() string {
	dataLabel := model.styles.Tab(apiDetailTabLabel(APIDetailTabData), model.apiList.Detail.ActiveTab == APIDetailTabData)
	issuesLabel := model.styles.Tab(apiDetailTabLabel(APIDetailTabIssues), model.apiList.Detail.ActiveTab == APIDetailTabIssues)
	return "/ " + dataLabel + " / " + issuesLabel
}

func apiDetailTabLabel(tab APIDetailTab) string {
	switch tab {
	case APIDetailTabData:
		return "DATA"
	case APIDetailTabIssues:
		return "ISSUES"
	default:
		return string(tab)
	}
}
