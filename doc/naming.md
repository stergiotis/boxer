# Naming Conventions

## Opposite Pairs
* Begin/End
* Prepare/Apply
* Start/Stop
* Incl/Excl
* Inclusive/Exclusive
* Add/Remove
* Create/Delete/Prune
* Create/Destroy
* Commit/Rollback
* Src/Dest
* Source/Destination
* First/Last
* Incr/Decr
* Increment/Decrement
* Lock/Unlock
* Next/Prev
* Old/New
* Open/Close
* Set/Get
* Set/Clear
* Set/Unset
* Show/Hide
* Up/Down
* Attach/Detach
* Compress/Decompress
* Connect/Disconnect
* Enable/Disable
* Encode/Decode
* Serialize/Deserialize
* Marshall/Unmarshall
* Inflate/Deflate
* Enter/Leave
* Freeze/Unfreeze
* Head/Tail
* Increase/Decrease
* Input/Output
* Ingress/Egress
* Prolog/Epilog
* Inbound/Outbound
* Link/Unlink
* Push/Pop
* Push/Pull
* Read/Write
* Register/Deregister
* Resume/Suspend
* Select/Deselect
* Send/Receive
* Setup/Teardown

## Go
Deviations from the golang language community naming conventions:
* Interfaces must end with `I`. I=Interface.
* Functions or methods without return values may end with `P`. P=Procedure.
* For functions or method pairs offering more and less advanced function argument sets: The name of the more advanced function or method must end with `V`. V=adVanced.
* Functions returning an error may end with `E`. E=Error.
* Only idempotent functions/methods are allowed to have a name starting with `Set`.
* Getters should have a name starting with `Get`.
* Predicates (functions and methods returning just a bool) should have a name starting with `Is`.
