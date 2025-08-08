<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0"
                xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
    <xsl:output method="xml" omit-xml-declaration="yes" indent="yes"/>
    <xsl:param name="apidefine">IMGUI_API</xsl:param>

    <xsl:template match="//memberdef[@kind='function']">
        <xsl:copy>
            <xsl:attribute name="API"><xsl:value-of select="count(.//ref[text() = $apidefine]) &gt; 0 or contains(./definition/text(), $apidefine)"/></xsl:attribute>
            <xsl:apply-templates select="node()|@*|text()"/>
        </xsl:copy>
    </xsl:template>

    <xsl:template match="//ref">
        <xsl:if test="text() != $apidefine">
            <xsl:value-of select="text()"/>
        </xsl:if>
    </xsl:template>

    <xsl:template match="node()|@*">
        <xsl:copy>
            <xsl:apply-templates select="node()|@*"/>
        </xsl:copy>
    </xsl:template>
</xsl:stylesheet>