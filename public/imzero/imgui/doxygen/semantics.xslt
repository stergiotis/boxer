<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0"
                xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
    <xsl:output method="xml" omit-xml-declaration="yes" indent="yes"/>

    <xsl:template match="//param">
        <xsl:copy>
            <xsl:variable name="isPOpen" select="declname = 'p_open' and type = 'bool *'"/>
            <xsl:attribute name="semantics">
                <xsl:choose>
                    <xsl:when test="$isPOpen">out</xsl:when>
                    <xsl:otherwise>in</xsl:otherwise>
                </xsl:choose>
            </xsl:attribute>
            <xsl:attribute name="cppInitialization">
                <xsl:choose>
                    <xsl:when test="$isPOpen">p_open = true; /* see issue #5 */</xsl:when>
                </xsl:choose>
            </xsl:attribute>
            <xsl:attribute name="castBegin">
                <xsl:choose>
                    <xsl:when test="type = 'ImGuiKey'"><xsl:value-of select="type"/>(</xsl:when>
                    <xsl:when test="type = 'ImGuiMouseSource'"><xsl:value-of select="type"/>(</xsl:when>
                    <xsl:when test="type = 'ImTextureID'"><xsl:value-of select="type"/>(</xsl:when>
                </xsl:choose>
            </xsl:attribute>
            <xsl:attribute name="castEnd">
                <xsl:choose>
                    <xsl:when test="type = 'ImGuiKey'">)</xsl:when>
                    <xsl:when test="type = 'ImGuiMouseSource'">)</xsl:when>
                    <xsl:when test="type = 'ImTextureID'">)</xsl:when>
                </xsl:choose>
            </xsl:attribute>
            <xsl:apply-templates select="node()|@*|text()"/>
        </xsl:copy>
    </xsl:template>

    <xsl:template match="node()|@*">
        <xsl:copy>
            <xsl:apply-templates select="node()|@*"/>
        </xsl:copy>
    </xsl:template>
</xsl:stylesheet>