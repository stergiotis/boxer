package fatruntime

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

type SectionIntrospectionI interface {
	GetSectionName() naming.StylableName
	GetSectionUseAspects() useaspects.AspectSet
	GetSectionStreamingGroup() naming.Key
	GetSectionCoSectionGroup() naming.Key
	GetSectionMembershipSpec() common.MembershipSpecE
}
