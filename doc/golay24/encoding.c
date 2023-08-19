// Telemetry Standards, IRIG Standard 106-15 (Part 1), Appendix Q, July 2015
#include "stdint.h"
#define GOLAY_SIZE 0x1000
// Generator matrix : parity sub-generator matrix P :
uint16_t G_P[12] = {
	0xc75, 0x63b, 0xf68, 0x7b4,
	0x3da, 0xd99, 0x6cd, 0x367,
	0xdc6, 0xa97, 0x93e, 0x8eb
};
/* Binary representation
   1 1 0 0 0 1 1 1 0 1 0 1
   0 1 1 0 0 0 1 1 1 0 1 1
   1 1 1 1 0 1 1 0 1 0 0 0
   0 1 1 1 1 0 1 1 0 1 0 0
   0 0 1 1 1 1 0 1 1 0 1 0
   1 1 0 1 1 0 0 1 1 0 0 1
   0 1 1 0 1 1 0 0 1 1 0 1
   0 0 1 1 0 1 1 0 0 1 1 1
   1 1 0 1 1 1 0 0 0 1 1 0
   1 0 1 0 1 0 0 1 0 1 1 1
   1 0 0 1 0 0 1 1 1 1 1 0
   1 0 0 0 1 1 1 0 1 0 1 1
   */
uint32_t EncodeTable[ GOLAY_SIZE ]; // Golay encoding table
				    // encode a 12-bit word to a 24-bit Golay code word
#define Encode(v) EncodeTable[v&0xfff]
// initialize the Golay encoding lookup table
void InitGolayEncode( void )
{
	for( uint32_t x=0; x < GOLAY_SIZE; x++ ) {
		// generate encode LUT
		EncodeTable[x]=(x<<12);
		for( uint32_t i=0; i<12; i++ ) {
			if( (x>>(11-i)) & 1 )
				EncodeTable[x] ^= G_P[i];
		}
	}
}

#include "stdio.h"
int main(void) {
	InitGolayEncode();
	int i;
	printf("package golay24\n\n");
	printf("var Encoding = [%d]uint32{\n",GOLAY_SIZE);
	for(i = 0;i<GOLAY_SIZE;i++){
		printf("0x%06x",EncodeTable[i]);
		if(i < GOLAY_SIZE-1) {
			if((i+1) % 8 == 0){
				printf(", \n");
			} else {
				printf(", ");
			}
		}
	}
	printf("}\n");
	return 0;
}
