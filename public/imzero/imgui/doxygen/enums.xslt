<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0"
                xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
    <xsl:output method="text" omit-xml-declaration="yes" indent="no"/>
    <xsl:param name="mode">auto</xsl:param>
    <xsl:param name="tags"></xsl:param>
    <xsl:param name="package"></xsl:param>
    <xsl:param name="goImports"></xsl:param>
    <xsl:param name="autoValueValue"></xsl:param>
    <xsl:param name="autoValueNameSuffix"></xsl:param>
    <xsl:param name="blacklist"></xsl:param>
    <xsl:variable name="lf"><xsl:text>
</xsl:text></xsl:variable>
    <xsl:variable name="blacklistS" select="concat(',',$blacklist,',')"/>

    <xsl:template match="/doxygen">
        <xsl:if test="$tags != ''">
            <xsl:value-of select="concat('//go:build ',$tags,$lf)"/>
        </xsl:if>
	<xsl:value-of select="concat('package ',$package,$lf,$lf)"/>
	<xsl:if test="$goImports != ''">
	    <xsl:value-of select="concat($goImports,$lf,$lf)"/>
	</xsl:if>

        <xsl:apply-templates/>
    </xsl:template>

    <xsl:template match="/doxygen/compounddef/sectiondef/memberdef[@kind='enum']">
        <xsl:variable name="type">
            <xsl:choose>
                <xsl:when test="substring(name,string-length(name)) = '_'">
                    <xsl:value-of select="substring(name,1,string-length(name)-1)"/>
                </xsl:when>
                <xsl:otherwise>
                    <xsl:value-of select="name"/>
                </xsl:otherwise>
            </xsl:choose>
        </xsl:variable>
        <xsl:choose>
            <xsl:when test="contains($blacklistS,concat(',',$type,','))">
                <xsl:message><xsl:value-of select="concat('skipping blacklisted type ',$type)"/></xsl:message>
            </xsl:when>
            <xsl:otherwise>
                <xsl:variable name="basictype">
                    <xsl:choose>
                        <xsl:when test="type = ''">int</xsl:when>
                        <xsl:otherwise><xsl:value-of select="type"/></xsl:otherwise>
                    </xsl:choose>
                </xsl:variable>
                <xsl:value-of select="concat('type ',$type,' ',$basictype,$lf)"/>
                <xsl:value-of select="concat('const (',$lf)"/>
                <xsl:for-each select="enumvalue">
                    <xsl:choose>
                        <xsl:when test="normalize-space(initializer)=''">
                            <xsl:value-of select="concat('  ',name,' = iota')"/>
                        </xsl:when>
                        <xsl:otherwise>
                            <xsl:value-of select="concat('  ',name,' = ',$type,'(',substring-after(initializer,'='),')')"/>
                        </xsl:otherwise>
                    </xsl:choose>
                    <xsl:variable name="comment" select="normalize-space(.//verbatim/text())"/>
                    <xsl:if test="$comment != ''">
                        <xsl:value-of select="concat(' // ', $comment)"/>
                    </xsl:if>
                    <xsl:value-of select="$lf"/>
                </xsl:for-each>
                <xsl:if test="$autoValueValue != ''">
                    <xsl:value-of select="concat('  ',$type,$autoValueNameSuffix,' ',$type,' = ',$autoValueValue,' // auto value',$lf)"/>
                </xsl:if>
                <xsl:value-of select="concat(')',$lf)"/>
            </xsl:otherwise>
        </xsl:choose>
    </xsl:template>

    <xsl:template match="text()"/>
</xsl:stylesheet>
