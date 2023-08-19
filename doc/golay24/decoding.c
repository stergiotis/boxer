// Telemetry Standards, IRIG Standard 106-15 (Part 1), Appendix Q, July 2015
#include "stdint.h"
#define GOLAY_SIZE 0x1000
uint16_t SyndromeTable[ GOLAY_SIZE ]; // Syndrome table
uint16_t CorrectTable[ GOLAY_SIZE ]; // correction table
uint8_t ErrorTable[ GOLAY_SIZE ]; // number of error bits table
#define Syndrome2(v1,v2) (SyndromeTable[v2]^(v1))
#define Syndrome(v) Syndrome2(((v)>>12)&0xfff,(v)&0xfff)
#define Errors2(v1,v2) ErrorTable[Syndrome2(v1,v2)]
#define Decode2(v1,v2) ((v1)^CorrectTable[Syndrome2(v1,v2)])
// get the number of error bits in this 24-bit code word
#define Errors(v) Errors2(((v)>>12)&0xfff,(v)&0xfff)
// get the 12-bit corrected code from a 24-bit code word
#define Decode(v) Decode2(((v)>>12)&0xfff,(v)&0xfff)
// Parity Check matrix
uint16_t H_P[12] = {
	0xa4f, 0xf68, 0x7b4, 0x3da,
	0x1ed, 0xab9, 0xf13, 0xdc6,
	0x6e3, 0x93e, 0x49f, 0xc75
};
/* Binary representation
   1 0 1 0 0 1 0 0 1 1 1 1
   1 1 1 1 0 1 1 0 1 0 0 0
   0 1 1 1 1 0 1 1 0 1 0 0
   0 0 1 1 1 1 0 1 1 0 1 0
   0 0 0 1 1 1 1 0 1 1 0 1
   1 0 1 0 1 0 1 1 1 0 0 1
   1 1 1 1 0 0 0 1 0 0 1 1
   1 1 0 1 1 1 0 0 0 1 1 0
   0 1 1 0 1 1 1 0 0 0 1 1
   1 0 0 1 0 0 1 1 1 1 1 0
   0 1 0 0 1 0 0 1 1 1 1 1
   1 1 0 0 0 1 1 1 0 1 0 1
   */
// calculate the number of 1s in a 24-bit word
uint8_t OnesInCode( uint32_t code, uint32_t size )
{
	uint8_t ret = 0;
	for( uint32_t i=0; i<size; i++ ) {
		if( (code>>i) & 1 )
			ret++;
	}
	return ret;
}
void InitGolayDecode( void )
{
	for( uint32_t x=0; x < GOLAY_SIZE; x++ ) {
		// generate syndrome LUT
		SyndromeTable[x]=0; // Default value of the Syndrome LUT
		for( uint32_t i=0; i<12; i++ ) {
			if( (x>>(11-i)) & 1 ) SyndromeTable[x] ^= H_P[i];
			ErrorTable[x] = 4;
			CorrectTable[x]=0xfff;
		}
	}
	// no error case
	ErrorTable[0] = 0;
	CorrectTable[0]= 0;
	// generate all error codes up to 3 ones
	for( int i=0; i<24; i++ ) {
		for( int j=0; j<24; j++ ) {
			for( int k=0; k<24; k++ ) {
				uint32_t error = (1<<i) | (1<<j) | (1<<k);
				uint32_t syndrome = Syndrome(error);
				CorrectTable[syndrome] = (error>>12) & 0xfff;
				ErrorTable[syndrome] = OnesInCode(error,24);
			}
		}
	}
}

#include "stdio.h"
#include "inttypes.h"
int main(void) {
	InitGolayDecode();
	int i;
	printf("package golay24\n\n");
	printf("var syndromTable = [%d]uint16{\n",GOLAY_SIZE);
	for(i=0;i<GOLAY_SIZE;i++) {
		printf("0x%04"PRIx16,SyndromeTable[i]);
		if(i < GOLAY_SIZE-1) {
			if((i+1) % 8 == 0) {
				printf(",\n");
			} else {
				printf(", ");
			}
		} else {
			printf("}\n");
		}
	}
	printf("\nvar correctTable = [%d]uint16{\n",GOLAY_SIZE);
	for(i=0;i<GOLAY_SIZE;i++) {
		printf("0x%04"PRIx16,CorrectTable[i]);
		if(i < GOLAY_SIZE-1) {
			if((i+1) % 8 == 0) {
				printf(",\n");
			} else {
				printf(", ");
			}
		} else {
			printf("}\n");
		}
	}
	printf("\nvar errorTable = [%d]uint8{\n",GOLAY_SIZE);
	for(i=0;i<GOLAY_SIZE;i++) {
		printf("%"PRId8,ErrorTable[i]);
		if(i < GOLAY_SIZE-1) {
			if((i+1) % 8 == 0) {
				printf(",\n");
			} else {
				printf(", ");
			}
		} else {
			printf("}\n");
		}
	}
	return 0;

}
