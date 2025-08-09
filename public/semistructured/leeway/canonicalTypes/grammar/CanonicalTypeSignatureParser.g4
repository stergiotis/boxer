/*
    Design notes:
    * Canonical types are designed to be texutally as short as possible
    * Groups are deliberately not composable.
    * Groups contain at least two elements
*/
parser grammar CanonicalTypeSignatureParser;
options { tokenVocab=CanonicalTypeSignatureLexer; }

baseString
    : UTF8_STRING
    | BYTE_STRING
    | BOOL
    ;

baseMachineNumeric
    : UNSIGNED
    | SIGNED
    | FLOAT;

baseTemporal
    : UTC_DATETIME
    | ZONED_DATETIME
    | ZONED_TIME;

scalarModifier
   : HOMOGENOUS_ARRAY
   | SET
   ;

byteOrderModifier
   : BIG_ENDIAN
   | LITTLE_ENDIAN
   ;

widthModifier
   : FIXED_MODIFIER NUMBER;

canonicalType
    : baseString widthModifier? scalarModifier?                      # CanonicalTypeString
    | baseTemporal NUMBER scalarModifier?                            # CanonicalTypeTemporal
    | baseMachineNumeric NUMBER byteOrderModifier? scalarModifier?   # CanonicalTypeMachineNumeric
    ;

canonicalTypeSequence
    : canonicalType (SEPARATOR canonicalType)*
    ;

canonicalTypeGroup
    :  canonicalType (GROUP_SEPARATOR canonicalType)*
    ;

canonicalTypeOrGroup
    :  canonicalType
    | canonicalTypeGroup
    ;

canonicalTypeOrGroupSequence
    :  canonicalTypeOrGroup (SEPARATOR canonicalTypeOrGroup)*
    ;

canonicalTypeSignature
    : canonicalTypeOrGroupSequence EOF
    ;
singleCanonicalType
    : canonicalType EOF
    ;
singleCanonicalTypeOrGroup
    : canonicalTypeOrGroup EOF
    ;
singleCanonicalGroup
    : canonicalTypeGroup EOF
    ;
