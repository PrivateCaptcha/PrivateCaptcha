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
	default:
		return strconv.Itoa(int(sc))
	}
}
