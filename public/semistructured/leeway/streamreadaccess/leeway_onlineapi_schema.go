//go:build llm_generated_opus47

package streamreadaccess

// DriveSchema walks the IR once, calling structural sink methods only.
//
// Compared to DriveRecordBatch, it never invokes BeginEntity/EndEntity,
// BeginPlainValue/EndPlainValue, BeginTaggedValue/EndTaggedValue, or
// any value-emitting methods. The signal "schema only, no entities follow"
// is `nAttrs == 0` on every BeginPlainSection / BeginSection call.
//
// Method-call shape:
//
//	BeginBatch
//	  for each plain section:
//	    BeginPlainSection(itemType, valueNames, valueCanonicalTypes, 0)
//	    EndPlainSection
//	  BeginTaggedSections
//	    for each co-section group:
//	      BeginCoSectionGroup(key)
//	        BeginSection(name, valueNames, valueCanonicalTypes, useAspects, 0)
//	        EndSection
//	      EndCoSectionGroup
//	    for each standalone tagged section:
//	      BeginSection(name, valueNames, valueCanonicalTypes, useAspects, 0)
//	      EndSection
//	  EndTaggedSections
//	EndBatch
//
// Co-grouped sections are visited in IR order within their group; standalone
// tagged sections are visited in IR order. The schema document emitter is
// responsible for any further sorting (e.g. lexicographic by section name).
//
// Errors are accumulated in inst.errs and returned merged at the end, like
// DriveRecordBatch.
func (inst *Driver) DriveSchema(sink SinkI) (err error) {
	inst.resetError()

	sink.BeginBatch()

	for psIdx := range inst.plainSections {
		ps := &inst.plainSections[psIdx]
		sink.BeginPlainSection(ps.itemType, ps.valueNames, ps.valueTypes, 0)
		errEnd := sink.EndPlainSection()
		inst.handleError(errEnd)
		if inst.hasError() {
			break
		}
	}

	if !inst.hasError() {
		sink.BeginTaggedSections()

		for gIdx := range inst.coGroups {
			group := &inst.coGroups[gIdx]
			sink.BeginCoSectionGroup(group.key)
			for _, sIdx := range group.sectionIds {
				sec := &inst.sections[sIdx]
				sink.BeginSection(sec.name, sec.valueNames, sec.valueTypes, sec.useAspects, 0)
				errEnd := sink.EndSection()
				inst.handleError(errEnd)
				if inst.hasError() {
					break
				}
			}
			if !inst.hasError() {
				errEnd := sink.EndCoSectionGroup()
				inst.handleError(errEnd)
			}
			if inst.hasError() {
				break
			}
		}

		if !inst.hasError() {
			for sIdx := range inst.sections {
				if inst.sectionInCoGroup[sIdx] >= 0 {
					continue
				}
				sec := &inst.sections[sIdx]
				sink.BeginSection(sec.name, sec.valueNames, sec.valueTypes, sec.useAspects, 0)
				errEnd := sink.EndSection()
				inst.handleError(errEnd)
				if inst.hasError() {
					break
				}
			}
		}

		errEnd := sink.EndTaggedSections()
		inst.handleError(errEnd)
	}

	errEnd := sink.EndBatch()
	inst.handleError(errEnd)

	err = inst.mergeAndClearError()
	return
}
