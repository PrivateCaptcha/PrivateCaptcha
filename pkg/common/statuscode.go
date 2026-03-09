package common

import "strconv"

type StatusCode int

const (
	// common errors
	StatusOK             StatusCode = 1000
	StatusFailure        StatusCode = 1001
	StatusUndefined      StatusCode = 1002
	StatusNotImplemented StatusCode = 1003
	StatusApiDeprecated  StatusCode = 1004
	// organization errors
	StatusOrgNameEmptyError          StatusCode = 1100
	StatusOrgNameTooLongError        StatusCode = 1101
	StatusOrgNameInvalidSymbolsError StatusCode = 1102
	StatusOrgNameDuplicateError      StatusCode = 1103
	StatusOrgLimitError              StatusCode = 1104
	StatusOrgNotFoundError           StatusCode = 1105
	StatusOrgPermissionsError        StatusCode = 1106
	StatusOrgIDNotEmptyError         StatusCode = 1107
	StatusOrgIDEmptyError            StatusCode = 1108
	StatusOrgIDInvalidError          StatusCode = 1109
	// properties errors
	StatusPropertiesTooManyError          StatusCode = 1200
	StatusPropertyNameEmptyError          StatusCode = 1201
	StatusPropertyNameTooLongError        StatusCode = 1202
	StatusPropertyNameInvalidSymbolsError StatusCode = 1203
	StatusPropertyNameDuplicateError      StatusCode = 1204
	StatusPropertyDomainEmptyError        StatusCode = 1205
	StatusPropertyDomainLocalhostError    StatusCode = 1206
	StatusPropertyDomainIPAddrError       StatusCode = 1207
	StatusPropertyDomainNameInvalidError  StatusCode = 1208
	StatusPropertyDomainResolveError      StatusCode = 1209
	StatusPropertyDomainFormatError       StatusCode = 1210
	StatusPropertyIDEmptyError            StatusCode = 1211
	StatusPropertyIDInvalidError          StatusCode = 1212
	StatusPropertyIDDuplicateError        StatusCode = 1213
	StatusPropertyPermissionsError        StatusCode = 1214
	// subscription errors
	StatusSubscriptionPropertyLimitError StatusCode = 1300
	// rules errors
	StatusRuleNameEmptyError                StatusCode = 1400
	StatusRuleConditionPropertyRequired     StatusCode = 1401
	StatusRuleConditionOperatorInvalid      StatusCode = 1402
	StatusRuleConditionValueRequired        StatusCode = 1403
	StatusRuleConditionPropertyInvalid      StatusCode = 1404
	StatusRuleActionPropertyRequired        StatusCode = 1405
	StatusRuleActionValueRequired           StatusCode = 1406
	StatusRuleActionValueInvalid            StatusCode = 1407
	StatusRuleActionPropertyInvalid         StatusCode = 1408
	StatusRuleIPAddressRequired             StatusCode = 1409
	StatusRuleCountryRequired               StatusCode = 1410
	StatusRuleCountryInvalid                StatusCode = 1411
	StatusRuleDomainRequired                StatusCode = 1412
	StatusRuleDomainInvalid                 StatusCode = 1413
	StatusRuleDomainSubdomain               StatusCode = 1414
	StatusRuleDifficultyValueInvalid        StatusCode = 1415
	StatusRuleDifficultyGrowthInvalid       StatusCode = 1416
	StatusRulePermissionsError              StatusCode = 1417
	StatusRulePositionPrecisionError        StatusCode = 1418
	StatusOrgRulesLimitError                StatusCode = 1419
	StatusPropertyRulesLimitError           StatusCode = 1420
	StatusOrgRulesSubscriptionRequired      StatusCode = 1421
	StatusPropertyRulesSubscriptionRequired StatusCode = 1422
	StatusRuleIPAddressInvalid              StatusCode = 1423
	StatusRuleIPAddressTooMany              StatusCode = 1424
	StatusRuleHTTPHeaderNameRequired        StatusCode = 1425
	StatusRuleHTTPHeaderNameInvalid         StatusCode = 1426
	StatusRuleNameInvalidCharsError         StatusCode = 1427
)

func (sc StatusCode) Success() bool {
	return sc == StatusOK
}

func (sc StatusCode) String() string {
	switch sc {
	case StatusOK:
		return "OK"
	case StatusFailure:
		return "Failure"
	case StatusUndefined:
		return "Undefined"
	case StatusNotImplemented:
		return "Not implemented"
	case StatusApiDeprecated:
		return "API is deprecated"
	case StatusOrgNameEmptyError:
		return "Name cannot be empty."
	case StatusOrgNameTooLongError:
		return "Name is too long."
	case StatusOrgNameInvalidSymbolsError:
		return "Organization name contains invalid characters."
	case StatusOrgNameDuplicateError:
		return "Organization with this name already exists."
	case StatusOrgLimitError:
		return "Organizations limit reached on your current plan, please upgrade to create more."
	case StatusOrgNotFoundError:
		return "Requested organization does not seem to exist."
	case StatusOrgPermissionsError:
		return "You do not have permissions to access this organization."
	case StatusOrgIDNotEmptyError:
		return "Organization ID must be empty."
	case StatusOrgIDEmptyError:
		return "Organization ID must not be empty."
	case StatusOrgIDInvalidError:
		return "Organization ID is not valid."
	case StatusPropertiesTooManyError:
		return "Properties batch limit size was exceeded."
	case StatusPropertyNameEmptyError:
		return "Name cannot be empty."
	case StatusPropertyNameTooLongError:
		return "Name is too long."
	case StatusPropertyNameInvalidSymbolsError:
		return "Property name contains invalid characters."
	case StatusPropertyNameDuplicateError:
		return "Property with this name already exists."
	case StatusPropertyDomainEmptyError:
		return "Domain name cannot be empty."
	case StatusPropertyDomainLocalhostError:
		return "Localhost is not allowed as a domain."
	case StatusPropertyDomainIPAddrError:
		return "IP address cannot be used as a domain."
	case StatusPropertyDomainNameInvalidError:
		return "Domain name is not valid."
	case StatusPropertyDomainResolveError:
		return "Failed to resolve domain name."
	case StatusPropertyDomainFormatError:
		return "Invalid format of domain name."
	case StatusPropertyIDEmptyError:
		return "Property ID cannot be empty."
	case StatusPropertyIDInvalidError:
		return "Property ID is not valid."
	case StatusPropertyIDDuplicateError:
		return "Duplicate property ID found in request."
	case StatusSubscriptionPropertyLimitError:
		return "Property limit reached for current subscription plan."
	case StatusPropertyPermissionsError:
		return "Insufficient permissions to update settings."
	case StatusRuleNameEmptyError:
		return "Rule name is required."
	case StatusRuleConditionPropertyRequired:
		return "Condition property is required."
	case StatusRuleConditionOperatorInvalid:
		return "Invalid operator for this condition property."
	case StatusRuleConditionValueRequired:
		return "Condition value is required for this operator."
	case StatusRuleConditionPropertyInvalid:
		return "Invalid condition property."
	case StatusRuleActionPropertyRequired:
		return "Action property is required."
	case StatusRuleActionValueRequired:
		return "Action value is required."
	case StatusRuleActionValueInvalid:
		return "Invalid action value."
	case StatusRuleActionPropertyInvalid:
		return "Invalid action property."
	case StatusRuleIPAddressRequired:
		return "IP address or prefix is required."
	case StatusRuleIPAddressInvalid:
		return "One or more IP address values are invalid."
	case StatusRuleIPAddressTooMany:
		return "Too many IP addresses provided."
	case StatusRuleCountryRequired:
		return "At least one country must be selected."
	case StatusRuleCountryInvalid:
		return "Country code is not valid."
	case StatusRuleDomainRequired:
		return "Domain is required."
	case StatusRuleDomainInvalid:
		return "Invalid domain name."
	case StatusRuleDomainSubdomain:
		return "Domain has to be a subdomain of main domain."
	case StatusRuleDifficultyValueInvalid:
		return "Difficulty adjustment must be within the range."
	case StatusRuleDifficultyGrowthInvalid:
		return "Difficulty growth must be a known value."
	case StatusRulePermissionsError:
		return "You don't have permission to access this rule."
	case StatusRulePositionPrecisionError:
		return "Rules need rebalancing. Please try again in a moment."
	case StatusOrgRulesLimitError:
		return "Organization rules limit reached on your current plan, please upgrade to create more."
	case StatusPropertyRulesLimitError:
		return "Property rules limit reached on your current plan, please upgrade to create more."
	case StatusOrgRulesSubscriptionRequired:
		return "You need an active subscription to create organization rules."
	case StatusPropertyRulesSubscriptionRequired:
		return "You need an active subscription to create property rules."
	case StatusRuleHTTPHeaderNameRequired:
		return "HTTP header name is required."
	case StatusRuleHTTPHeaderNameInvalid:
		return "HTTP header name is not valid."
	case StatusRuleNameInvalidCharsError:
		return "Rule name can only contain letters, numbers, and spaces."
	default:
		return strconv.Itoa(int(sc))
	}
}
