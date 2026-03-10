package common

import (
	"testing"
)

func TestStatusCodeString(t *testing.T) {
	statusCodes := []StatusCode{
		StatusOK,
		StatusFailure,
		StatusUndefined,
		StatusNotImplemented,
		StatusApiDeprecated,
		StatusOrgNameEmptyError,
		StatusOrgNameTooLongError,
		StatusOrgNameInvalidSymbolsError,
		StatusOrgNameDuplicateError,
		StatusOrgLimitError,
		StatusOrgNotFoundError,
		StatusOrgPermissionsError,
		StatusOrgIDNotEmptyError,
		StatusOrgIDEmptyError,
		StatusOrgIDInvalidError,
		StatusPropertiesTooManyError,
		StatusPropertyNameEmptyError,
		StatusPropertyNameTooLongError,
		StatusPropertyNameInvalidSymbolsError,
		StatusPropertyNameDuplicateError,
		StatusPropertyDomainEmptyError,
		StatusPropertyDomainLocalhostError,
		StatusPropertyDomainIPAddrError,
		StatusPropertyDomainNameInvalidError,
		StatusPropertyDomainResolveError,
		StatusPropertyDomainFormatError,
		StatusPropertyIDEmptyError,
		StatusPropertyIDInvalidError,
		StatusPropertyIDDuplicateError,
		StatusPropertyPermissionsError,
		StatusSubscriptionPropertyLimitError,
		StatusRuleNameEmptyError,
		StatusRuleConditionPropertyRequired,
		StatusRuleConditionOperatorInvalid,
		StatusRuleConditionValueRequired,
		StatusRuleConditionPropertyInvalid,
		StatusRuleActionPropertyRequired,
		StatusRuleActionValueRequired,
		StatusRuleActionValueInvalid,
		StatusRuleActionPropertyInvalid,
		StatusRuleIPAddressRequired,
		StatusRuleIPAddressInvalid,
		StatusRuleIPAddressTooMany,
		StatusRuleCountryRequired,
		StatusRuleCountryInvalid,
		StatusRuleDifficultyValueInvalid,
		StatusRuleDifficultyGrowthInvalid,
		StatusRuleDomainRequired,
		StatusRuleDomainInvalid,
		StatusRuleDomainSubdomain,
		StatusRulePermissionsError,
		StatusRulePositionPrecisionError,
		StatusPropertyRulesLimitError,
		StatusPropertyRulesSubscriptionRequired,
		StatusRuleHTTPHeaderNameRequired,
		StatusRuleHTTPHeaderNameInvalid,
		StatusRuleNameInvalidCharsError,
	}

	for _, sc := range statusCodes {
		t.Run(sc.String(), func(t *testing.T) {
			str := sc.String()
			if len(str) == 0 {
				t.Errorf("String() returned empty string for status code %d", sc)
			}
		})
	}
}

func TestStatusCodeSuccess(t *testing.T) {
	if !StatusOK.Success() {
		t.Error("StatusOK.Success() should return true")
	}

	if StatusFailure.Success() {
		t.Error("StatusFailure.Success() should return false")
	}

	if StatusOrgNameEmptyError.Success() {
		t.Error("StatusOrgNameEmptyError.Success() should return false")
	}
}

func TestUnknownStatusCode(t *testing.T) {
	unknown := StatusCode(99999)
	str := unknown.String()
	if str != "99999" {
		t.Errorf("Unknown status code should return numeric string, got %s", str)
	}
}

func TestRegisterStatusCodes(t *testing.T) {
	customCode := StatusCode(2401)

	t.Cleanup(func() {
		delete(extraStatusCodeStrings, customCode)
	})

	// Before registration, should return numeric string
	if str := customCode.String(); str != "2401" {
		t.Errorf("Unregistered status code should return numeric string, got %s", str)
	}

	RegisterStatusCodes(map[StatusCode]string{
		customCode: "Custom error message.",
	})

	// After registration, should return the registered string
	if str := customCode.String(); str != "Custom error message." {
		t.Errorf("Registered status code should return registered string, got %s", str)
	}

	// Built-in codes should still work
	if str := StatusOK.String(); str != "OK" {
		t.Errorf("Built-in status code should still work, got %s", str)
	}
}
