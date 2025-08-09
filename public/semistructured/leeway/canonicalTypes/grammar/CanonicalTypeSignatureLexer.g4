lexer grammar CanonicalTypeSignatureLexer;

SEPARATOR : [ _,;]+;
GROUP_SEPARATOR : '-';

UTF8_STRING : 's';
BYTE_STRING : 'y';
BOOL : 'b';
UNSIGNED : 'u';
SIGNED : 'i';
FLOAT : 'f';
UTC_DATETIME : 'z'; // z for UTC in ISO 8601
ZONED_DATETIME : 'd';
ZONED_TIME : 't';
HOMOGENOUS_ARRAY : 'h';
SET : 'm'; // m for german "menge"
LITTLE_ENDIAN : 'l';
BIG_ENDIAN : 'n'; // network byte order
FIXED_MODIFIER : 'x';

/*NONZERO_DIGIT : '1'..'9';
DIGITS : '0'..'9'+;
NUMBER : NONZERO_DIGIT DIGITS?;
*/
NUMBER : [1-9] [0-9]*;