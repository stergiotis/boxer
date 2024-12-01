package cbor

import (
	"hash"
	"math/rand/v2"
	"strings"

	gofakeit "github.com/brianvoe/gofakeit/v7"
	"github.com/zeebo/xxh3"
)

type Generator struct {
	MaxNestingLevel    int
	Hasher             hash.Hash64
	Enc                *Encoder
	MaxTotalPrimitives int
	maxStringLength    int
	rand               *rand.Rand
	seed               int64
	nPrimitives        int
	faker              *gofakeit.Faker
	stringGenerators   []func() string
}

func NewGenerator(w EncoderWriter, randSeed int64) *Generator {
	const maxStringLength = 4 * 1024
	src := rand.NewPCG(uint64(randSeed), uint64(-randSeed))
	ra := rand.New(src)
	hasher := xxh3.New()
	r := &Generator{
		MaxNestingLevel:    8,
		MaxTotalPrimitives: 1000,
		Enc:                NewEncoder(w, hasher),
		rand:               ra,
		seed:               randSeed,
		Hasher:             hasher,
		nPrimitives:        0,
		maxStringLength:    maxStringLength,
		faker:              gofakeit.NewFaker(src, false),
		stringGenerators:   nil,
	}
	r.SetMaxStringLength(maxStringLength)
	return r
}

func (inst *Generator) SetMaxStringLength(n int) {
	if n < 0 {
		return
	}
	inst.maxStringLength = n

	if inst.stringGenerators == nil {
		sg := make([]func() string, 0, 300)
		add := func(f func() string) {
			sg = append(sg, f)
		}
		f := inst.faker
		add(f.ProductName)
		add(f.ProductDescription)
		add(f.ProductCategory)
		add(f.ProductFeature)
		add(f.ProductMaterial)
		add(f.ProductUPC)
		add(f.ProductDimension)
		add(f.ProductUseCase)
		add(f.ProductBenefit)
		add(f.ProductSuffix)
		add(f.Name)
		add(f.NamePrefix)
		add(f.NameSuffix)
		add(f.FirstName)
		add(f.MiddleName)
		add(f.LastName)
		add(f.Gender)
		add(f.SSN)
		add(f.Hobby)
		add(f.Email)
		add(f.Phone)
		add(f.PhoneFormatted)
		add(f.Username)
		add(f.City)
		add(f.Country)
		add(f.CountryAbr)
		add(f.State)
		add(f.StateAbr)
		add(f.Street)
		add(f.StreetName)
		add(f.StreetNumber)
		add(f.StreetPrefix)
		add(f.StreetSuffix)
		add(f.Zip)
		add(f.Gamertag)
		add(f.BeerAlcohol)
		add(f.BeerBlg)
		add(f.BeerHop)
		add(f.BeerIbu)
		add(f.BeerMalt)
		add(f.BeerName)
		add(f.BeerStyle)
		add(f.BeerYeast)
		add(f.CarMaker)
		add(f.CarModel)
		add(f.CarType)
		add(f.CarFuelType)
		add(f.CarTransmissionType)
		add(f.Noun)
		add(f.NounCommon)
		add(f.NounConcrete)
		add(f.NounAbstract)
		add(f.NounCollectivePeople)
		add(f.NounCollectiveAnimal)
		add(f.NounCollectiveThing)
		add(f.NounCountable)
		add(f.NounUncountable)
		add(f.Verb)
		add(f.VerbAction)
		add(f.VerbLinking)
		add(f.VerbHelping)
		add(f.Adverb)
		add(f.AdverbManner)
		add(f.AdverbDegree)
		add(f.AdverbPlace)
		add(f.AdverbTimeDefinite)
		add(f.AdverbTimeIndefinite)
		add(f.AdverbFrequencyDefinite)
		add(f.AdverbFrequencyIndefinite)
		add(f.Preposition)
		add(f.PrepositionSimple)
		add(f.PrepositionDouble)
		add(f.PrepositionCompound)
		add(f.Adjective)
		add(f.AdjectiveDescriptive)
		add(f.AdjectiveQuantitative)
		add(f.AdjectiveProper)
		add(f.AdjectiveDemonstrative)
		add(f.AdjectivePossessive)
		add(f.AdjectiveInterrogative)
		add(f.AdjectiveIndefinite)
		add(f.Pronoun)
		add(f.PronounPersonal)
		add(f.PronounObject)
		add(f.PronounPossessive)
		add(f.PronounReflective)
		add(f.PronounDemonstrative)
		add(f.PronounInterrogative)
		add(f.PronounRelative)
		add(f.Connective)
		add(f.ConnectiveTime)
		add(f.ConnectiveComparative)
		add(f.ConnectiveComplaint)
		add(f.ConnectiveListing)
		add(f.ConnectiveCasual)
		add(f.ConnectiveExamplify)
		add(f.Word)
		add(f.LoremIpsumWord)
		add(f.Question)
		add(f.Quote)
		add(f.Phrase)
		add(f.Fruit)
		add(f.Vegetable)
		add(f.Breakfast)
		add(f.Lunch)
		add(f.Dinner)
		add(f.Snack)
		add(f.Dessert)
		add(f.UUID)
		add(f.FlipACoin)
		add(f.Color)
		add(f.HexColor)
		add(f.SafeColor)
		add(f.URL)
		add(f.DomainName)
		add(f.DomainSuffix)
		add(f.IPv4Address)
		add(f.IPv6Address)
		add(f.MacAddress)
		add(f.HTTPMethod)
		add(f.HTTPVersion)
		add(f.UserAgent)
		add(f.ChromeUserAgent)
		add(f.FirefoxUserAgent)
		add(f.OperaUserAgent)
		add(f.SafariUserAgent)
		add(f.InputName)
		add(f.MonthString)
		add(f.WeekDay)
		add(f.TimeZone)
		add(f.TimeZoneAbv)
		add(f.TimeZoneFull)
		add(f.TimeZoneRegion)
		add(f.CreditCardCvv)
		add(f.CreditCardExp)
		add(f.CreditCardType)
		add(f.CurrencyLong)
		add(f.CurrencyShort)
		add(f.AchRouting)
		add(f.AchAccount)
		add(f.BitcoinAddress)
		add(f.BitcoinPrivateKey)
		add(f.Cusip)
		add(f.Isin)
		add(f.BS)
		add(f.Blurb)
		add(f.BuzzWord)
		add(f.Company)
		add(f.CompanySuffix)
		add(f.JobDescriptor)
		add(f.JobLevel)
		add(f.JobTitle)
		add(f.Slogan)
		add(f.HackerAbbreviation)
		add(f.HackerAdjective)
		add(f.HackeringVerb)
		add(f.HackerNoun)
		add(f.HackerPhrase)
		add(f.HackerVerb)
		add(f.HipsterWord)
		add(f.AppName)
		add(f.AppVersion)
		add(f.AppAuthor)
		add(f.PetName)
		add(f.Animal)
		add(f.AnimalType)
		add(f.FarmAnimal)
		add(f.Cat)
		add(f.Dog)
		add(f.Bird)
		add(f.Emoji)
		add(f.EmojiDescription)
		add(f.EmojiCategory)
		add(f.EmojiAlias)
		add(f.EmojiTag)
		add(f.Language)
		add(f.LanguageAbbreviation)
		add(f.ProgrammingLanguage)
		add(f.Digit)
		add(f.Letter)
		add(f.CelebrityActor)
		add(f.CelebrityBusiness)
		add(f.CelebritySport)
		add(f.MinecraftOre)
		add(f.MinecraftWood)
		add(f.MinecraftArmorTier)
		add(f.MinecraftArmorPart)
		add(f.MinecraftWeapon)
		add(f.MinecraftTool)
		add(f.MinecraftDye)
		add(f.MinecraftFood)
		add(f.MinecraftAnimal)
		add(f.MinecraftVillagerJob)
		add(f.MinecraftVillagerStation)
		add(f.MinecraftVillagerLevel)
		add(f.MinecraftMobPassive)
		add(f.MinecraftMobNeutral)
		add(f.MinecraftMobHostile)
		add(f.MinecraftMobBoss)
		add(f.MinecraftBiome)
		add(f.MinecraftWeather)
		add(f.BookTitle)
		add(f.BookAuthor)
		add(f.BookGenre)
		add(f.MovieName)
		add(f.MovieGenre)
		add(f.School)
		inst.stringGenerators = sg
	}
}

func (inst *Generator) Reset() {
	inst.Enc.Reset()
	inst.nPrimitives = 0
}
func (inst *Generator) generateString() string {
	sg := inst.stringGenerators
	s := sg[inst.rand.IntN(len(sg))]()
	if len(s) > inst.maxStringLength {
		// FIXME this may be slow, only the last codepoint may be torn...
		return strings.ToValidUTF8(s[:inst.maxStringLength], "-")
	}
	return s
}
func (inst *Generator) generateBytes() []byte {
	return inst.faker.ImagePng(inst.rand.IntN(100)+10, 20)
}

func (inst *Generator) GenerateRandomCborScalar() (n int, err error) {
	var u int
	enc := inst.Enc
	ra := inst.rand
	switch ra.IntN(16) {
	case 0:
		u, err = enc.EncodeByteSlice(inst.generateBytes())
		n += u
		if err != nil {
			return
		}
		break
	case 1, 2, 3, 4, 5, 6, 7, 8:
		u, err = enc.EncodeString(inst.generateString())
		n += u
		if err != nil {
			return
		}
		break
	case 9:
		u, err = enc.EncodeBool(ra.Float32() < 0.5)
		n += u
		if err != nil {
			return
		}
		break
	case 10, 11, 12:
		u, err = enc.EncodeInt(ra.Int64())
		n += u
		if err != nil {
			return
		}
		break
	case 13, 14, 15, 16:
		u, err = enc.EncodeUint(ra.Uint64())
		n += u
		if err != nil {
			return
		}
		break
	}
	return n, nil
}

func (inst *Generator) GenerateRandomCbor() (n int, err error) {
	return inst.generateRandomCbor(0)
}

func (inst *Generator) generateRandomCbor(level int) (n int, err error) {
	u := 0
	enc := inst.Enc
	maxLevel := inst.MaxNestingLevel
	if level >= maxLevel {
		inst.nPrimitives++
		u, err = inst.GenerateRandomCborScalar()
		n += u
		if err != nil {
			return
		}
		return
	}
	ra := inst.rand
	t := ra.IntN(12)
	maxL := inst.nextMaxContainerSize()
	if maxL <= 0 {
		t = 6 // generate scalars only
	}

	switch t {
	case 0:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeArrayDefinite(uint64(l))
		n += u
		if err != nil {
			return
		}
		for i := 0; i < l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		break
	case 1:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeArrayIndefinite()
		n += u
		if err != nil {
			return
		}
		for i := 0; i < l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		u, err = enc.EncodeBreak()
		n += u
		break
	case 2:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeMapDefinite(uint64(l))
		n += u
		if err != nil {
			return
		}
		for i := 0; i < 2*l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		break
	case 3:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeMapIndefinite()
		n += u
		if err != nil {
			return
		}
		for i := 0; i < 2*l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		u, err = enc.EncodeBreak()
		n += u
		break
	case 4:
		u, err = enc.EncodeTagSmall(TagExpectConversionToBase64Std)
		n += u
		if err != nil {
			return
		}
		u, err = enc.EncodeByteSlice(inst.generateBytes())
		n += u
		if err != nil {
			return
		}
		break
	default:
		u, err = inst.GenerateRandomCborScalar()
		n += u
		if err != nil {
			return
		}
	}
	return
}
func (inst *Generator) nextMaxContainerSize() (nMaxLength int) {
	d := inst.MaxTotalPrimitives - inst.nPrimitives
	if d <= 0 {
		return 0
	} else if d > 12 {
		return 12
	}
	return d
}
