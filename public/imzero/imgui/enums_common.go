package imgui

type ItemStatusE uint16

const (
	ItemStatusFocused          ItemStatusE = 1 << 0
	ItemStatusHoveredPrimary   ItemStatusE = 1 << 1
	ItemStatusHoveredSecondary ItemStatusE = 1 << 2
	ItemStatusActive           ItemStatusE = 1 << 3
	// ItemStatusEdited Value exposed by item was edited in the current frame (should match the bool return value of most widgets)
	ItemStatusEdited    ItemStatusE = 1 << 4
	ItemStatusActivated ItemStatusE = 1 << 5
	// ItemStatusDeactivated Only valid if ImGuiItemStatusFlags_HasDeactivated is set.
	ItemStatusDeactivated          ItemStatusE = 1 << 6
	ItemStatusDeactivatedAfterEdit ItemStatusE = 1 << 7
	// ItemStatusVisible [WIP] Set when item is overlapping the current clipping rectangle (Used internally. Please don't use yet: API/system will change as we refactor Itemadd()).
	ItemStatusVisible ItemStatusE = 1 << 8
	ItemStatusClicked ItemStatusE = 1 << 9
	// ItemStatusToggleOpen Set when TreeNode() reports toggling their open state.
	ItemStatusToggleOpen ItemStatusE = 1 << 10

	// ItemStatusDisabled this is not an imgui status but an ImGuiItemFlag
	ItemStatusDisabled ItemStatusE = 1 << 11
)

func (status ItemStatusE) IsFocused() bool {
	return status&ItemStatusFocused != 0
}

func (status ItemStatusE) IsHoveredPrimary() bool {
	return status&ItemStatusHoveredPrimary != 0
}

func (status ItemStatusE) IsHoveredSecondary() bool {
	return status&ItemStatusHoveredSecondary != 0
}

func (status ItemStatusE) IsActive() bool {
	return status&ItemStatusActive != 0
}

func (status ItemStatusE) IsEdited() bool {
	return status&ItemStatusEdited != 0
}

func (status ItemStatusE) IsActivated() bool {
	return status&ItemStatusActivated != 0
}

func (status ItemStatusE) IsDeactivated() bool {
	return status&ItemStatusDeactivated != 0
}

func (status ItemStatusE) IsDeactivatedAfterEdit() bool {
	return status&ItemStatusDeactivatedAfterEdit != 0
}

func (status ItemStatusE) IsVisible() bool {
	return status&ItemStatusVisible != 0
}

func (status ItemStatusE) IsClicked() bool {
	return status&ItemStatusClicked != 0
}

func (status ItemStatusE) IsToggleOpen() bool {
	return status&ItemStatusToggleOpen != 0
}

func (status ItemStatusE) IsDisabled() bool {
	return status&ItemStatusDisabled != 0
}
