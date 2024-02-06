<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0"
                xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
    <xsl:output method="text" omit-xml-declaration="yes" indent="no"/>
    <xsl:param name="mode">auto</xsl:param>
    <xsl:param name="file"></xsl:param>
    <xsl:param name="tags"></xsl:param>
    <xsl:param name="package"></xsl:param>
    <xsl:param name="instvar"></xsl:param>
    <xsl:param name="instvarns"></xsl:param>
    <xsl:param name="goImports"></xsl:param>
    <xsl:variable name="lf"><xsl:text>
</xsl:text></xsl:variable>

    <xsl:template name="goDecl">
        <xsl:param name="pmode" select="'mandatory'"/>
        <xsl:param name="suffix"><xsl:if test="$pmode='optional'">V</xsl:if></xsl:param>
	<xsl:choose>
		<xsl:when test="$instvar = ''">
        		<xsl:value-of select="concat('func ',name,$suffix, '(')"/>
		</xsl:when>
		<xsl:otherwise>
        		<xsl:value-of select="concat('func (',$instvar, ') ',name,$suffix, '(')"/>
		</xsl:otherwise>
	</xsl:choose>
        <xsl:variable name="nParamsMand" select="count(param[not(defval) and @semantics = 'in'])"/>
	<xsl:variable name="nParamsOpt" select="count(param[defval and @semantics = 'in'])"/>


        <!-- mandatory arguments -->
        <xsl:for-each select="param[not(defval) and @semantics = 'in']">
 	    <xsl:if test="not(declname)">/* FIXME unnamed param */</xsl:if>
            <xsl:value-of select="concat(declname,' ')" />
            <xsl:call-template name="resolveType">
                <xsl:with-param name="type" select="normalize-space(type/text())"/>
                <xsl:with-param name="array" select="array"/>
            </xsl:call-template>
            <xsl:if test="position() &lt; $nParamsMand">
                <xsl:value-of select="','" />
            </xsl:if>
        </xsl:for-each>
        <!-- optional arguments -->
        <xsl:if test="$pmode='optional'">
            <xsl:if test="$nParamsMand &gt; 0"><xsl:text>,</xsl:text></xsl:if>
            <xsl:for-each select="param[defval and @semantics='in']">
 	        <xsl:if test="not(declname)">/* FIXME unnamed param */</xsl:if>
                <xsl:value-of select="concat('',declname,' ')" />
                <xsl:call-template name="resolveType">
                    <xsl:with-param name="type" select="normalize-space(type/text())"/>
                    <xsl:with-param name="array" select="array"/>
                </xsl:call-template>
                <xsl:value-of select="concat(' /* = ',defval,'*/')"/>
                <xsl:if test="position() &lt; $nParamsOpt">
                    <xsl:value-of select="','" />
                </xsl:if>
            </xsl:for-each>
        </xsl:if>
        <xsl:value-of select="')'"/>
    </xsl:template>
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

    <xsl:template name="goCodeDriver">
        <xsl:param name="pmode"/>
        <xsl:variable name="nParamsMand" select="count(param[not(defval) and @semantics='in'])"/>
        <xsl:variable name="nParamsTotal" select="count(param[@semantics='in'])"/>
        <xsl:choose>
            <xsl:when test="$pmode = 'optional' and $nParamsMand = $nParamsTotal">
                <!-- skipping function in optional param mode: no optional params -->
            </xsl:when>
            <xsl:when test="name = 'Value'">
                <xsl:message>
                    <xsl:value-of select="concat('skipping function: blacklisted ',name)"/>
                </xsl:message>
            </xsl:when>
            <xsl:when test="count(param[type = 'const char *' and contains(declname, '_end')]) &gt; 0">
                <xsl:message>
                    <xsl:value-of select="concat('skipping function: string begin/end pointer semantics ',name,' ',argsstring)"/>
                </xsl:message>
            </xsl:when>
            <xsl:when test="contains(argsstring/text(), 'IM_FMT')">
                <xsl:message>
                    <xsl:value-of select="concat('skipping function: format string found: ',name,' ',argsstring/text())"/>
                </xsl:message>
            </xsl:when>
            <xsl:otherwise>
                <xsl:call-template name="goCode">
                    <xsl:with-param name="pmode" select="$pmode"/>
                </xsl:call-template>
            </xsl:otherwise>
        </xsl:choose>
    </xsl:template>

    <xsl:template name="emitParamCode">
        <xsl:value-of select="@castBegin"/>
        <xsl:choose>
            <xsl:when test="@semantics='out'"><xsl:value-of select="concat('&amp;',declname)"/></xsl:when>
            <xsl:otherwise>
                <xsl:value-of select="declname" />
            </xsl:otherwise>
        </xsl:choose>
        <xsl:value-of select="@castEnd"/>
    </xsl:template>

    <xsl:template name="goCode">
        <xsl:param name="pmode" select="'mandatory'"/>
        <xsl:param name="suffix"><xsl:if test="$pmode='optional'">V</xsl:if></xsl:param>
        <xsl:variable name="type">
            <xsl:call-template name="resolveType">
                <xsl:with-param name="type" select="normalize-space(type/text())"/>
                <xsl:with-param name="array" select="array"/>
            </xsl:call-template>
        </xsl:variable>
        <xsl:variable name="goDecl">
            <xsl:call-template name="goDecl">
                <xsl:with-param name="pmode" select="$pmode"/>
            </xsl:call-template>
        </xsl:variable>
        <xsl:variable name="docDefaultParams">
            <xsl:if test="$pmode='optional'">
                <xsl:for-each select="param[defval]">
                    <xsl:value-of select="concat('// * ',declname,' ',type,' = ',defval,$lf)"/>
                </xsl:for-each>
            </xsl:if>
        </xsl:variable>
        <xsl:variable name="doc" select="concat(normalize-space(briefdescription),normalize-space(detaileddescription))"/>
        <xsl:if test="$doc != ''">
            <xsl:value-of select="concat('// ',name,$suffix, ' ',$doc,$lf,$docDefaultParams)"/>
        </xsl:if>
        <xsl:variable name="nParams" select="count(param[not(defval) or $pmode='optional'])"/>
        <xsl:variable name="resultsEpilog">
            <xsl:for-each select="param[(not(defval) or $pmode='optional') and @semantics='out']">
                <xsl:if test="type = 'bool *'"><xsl:value-of select="concat(declname,' bool')"/></xsl:if>
            </xsl:for-each>
    	</xsl:variable>
	<xsl:variable name="callee">
		<xsl:choose>
			<xsl:when test="$instvar=''"><xsl:value-of select="qualifiedname"/></xsl:when>
			<xsl:otherwise><xsl:value-of select="concat('((',$instvarns,substring-before(qualifiedname,'::'),'*)foreignptr)->', substring-after(qualifiedname,'::'))"/></xsl:otherwise>
		</xsl:choose>
	</xsl:variable>
        <xsl:choose>
            <xsl:when test="$type = 'void'">
                <xsl:choose>
                    <xsl:when test="$resultsEpilog = ''">
                        <xsl:value-of select="concat($goDecl,' {',$lf)"/>
                    </xsl:when>
                    <xsl:otherwise>
                        <xsl:value-of select="concat($goDecl,'(',$resultsEpilog,') {',$lf)"/>
                    </xsl:otherwise>
                </xsl:choose>

                <!-- cpp -->
                <xsl:value-of select="concat('  _ = `',$callee,'(')" />
                <xsl:for-each select="param[not(defval) or $pmode='optional']">
                    <xsl:call-template name="emitParamCode"/>
                    <xsl:if test="position() &lt; $nParams">
                        <xsl:value-of select="', '" />
                    </xsl:if>
                </xsl:for-each>
                <xsl:value-of select="concat(')`',$lf)"/>

                <xsl:value-of select="concat('}',$lf)"/>
            </xsl:when>
            <xsl:otherwise>
                <xsl:choose>
                    <xsl:when test="$resultsEpilog = ''">
                        <xsl:value-of select="concat($goDecl,' (r ',$type,') {',$lf)"/>
                    </xsl:when>
                    <xsl:otherwise>
                        <xsl:value-of select="concat($goDecl,' (r ',$type,',',$resultsEpilog,') {',$lf)"/>
                    </xsl:otherwise>
                </xsl:choose>

                <!-- cpp -->
                <xsl:value-of select="concat('  _ = `','auto r = ',$callee,'(')" />
                <xsl:for-each select="param[not(defval) or $pmode='optional']">
                    <xsl:call-template name="emitParamCode"/>
                    <xsl:if test="position() &lt; $nParams">
                        <xsl:value-of select="', '" />
                    </xsl:if>
                </xsl:for-each>
                <xsl:value-of select="concat(')`',$lf)"/>
                <xsl:value-of select="concat('  return',$lf)"/>

                <xsl:value-of select="concat('}',$lf)"/>
            </xsl:otherwise>
        </xsl:choose>

        <xsl:apply-templates/>
    </xsl:template>

    <xsl:template name="resolveType">
        <xsl:param name="type"/>
        <xsl:param name="array"/>

        <xsl:value-of select="$array"/>
        <xsl:variable name="isConst" select="starts-with($type, 'const ')"/>
        <xsl:variable name="type2">
            <xsl:choose>
                <xsl:when test="$isConst"><xsl:value-of select="normalize-space(translate(substring-after($type,'const '),'&amp;',''))"/></xsl:when>
                <xsl:otherwise><xsl:value-of select="$type"/></xsl:otherwise>
            </xsl:choose>
        </xsl:variable>
        <xsl:variable name="r">
            <xsl:choose>
                <xsl:when test="$array != '' and $isConst != true()"><xsl:value-of select="$type"/> /* FIXME: by ref array */</xsl:when>
                <xsl:when test="$type = 'const char *'">string</xsl:when>
                <xsl:when test="$type = 'const ImColor &amp;'">uint32</xsl:when>

                <xsl:when test="$type2 = 'float'">float32</xsl:when>
                <xsl:when test="$type2 = 'double'">float64</xsl:when>
                <xsl:when test="$type2 = 'bool'">bool</xsl:when>
                <xsl:when test="$type2 = 'int'"><xsl:value-of select="'int'"/></xsl:when>
                <xsl:when test="$type2 = 'void'">void</xsl:when>
                <xsl:when test="$type2 = 'size_t'">Size_t</xsl:when>

                <xsl:when test="contains($type2,'Flags')"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="substring($type2, string-length($type2) - 1 +1) = 'E'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = ''"><xsl:value-of select="$type"/></xsl:when>

                <!-- imgui -->
                <xsl:when test="$type2 = 'ImVec2'">ImVec2</xsl:when>
                <xsl:when test="$type2 = 'ImVec4'">ImVec4</xsl:when>
                <xsl:when test="$type2 = 'ImGuiID'">ImGuiID</xsl:when>
                <xsl:when test="$type2 = 'ImWchar16'">ImWchar16</xsl:when>
                <xsl:when test="$type2 = 'ImWchar32'">ImWchar32</xsl:when>
                <xsl:when test="$type2 = 'ImS8'">int8</xsl:when>
                <xsl:when test="$type2 = 'ImU8'">uint8</xsl:when>
                <xsl:when test="$type2 = 'ImS16'">int16</xsl:when>
                <xsl:when test="$type2 = 'ImU16'">uint16</xsl:when>
                <xsl:when test="$type2 = 'ImS32'">int32</xsl:when>
                <xsl:when test="$type2 = 'ImU32'">uint32</xsl:when>
                <xsl:when test="$type2 = 'ImS64'">int64</xsl:when>
                <xsl:when test="$type2 = 'ImU32'"><xsl:value-of select="'uint32'"/></xsl:when>
                <xsl:when test="$type2 = 'ImU64'">uint64</xsl:when>
                <xsl:when test="$type2 = 'ImTextureID'">ImTextureID</xsl:when>
                <xsl:when test="$type2 = 'ImDrawList *'">ImDrawListPtr</xsl:when>
                <xsl:when test="$type2 = 'ImGuiTableBgTarget'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiSortDirection'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiKey'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiNavInput'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiMouseButton'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiMouseCursor'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiMouseSource'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiCond'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiCol'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiDir'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiDataType'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImGuiStyleVar'"><xsl:value-of select="$type2"/></xsl:when>

                <!-- ImPlot-->
                <xsl:when test="$type2 = 'ImPlotPoint'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImAxis'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotCond'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotCol'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotStyleVar'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotScale'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotMarker'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotColormap'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotLocation'"><xsl:value-of select="$type2"/></xsl:when>
                <xsl:when test="$type2 = 'ImPlotBin'"><xsl:value-of select="$type2"/></xsl:when>

                <xsl:otherwise>/* FIXME */ <xsl:value-of select="$type"/></xsl:otherwise>
            </xsl:choose>
        </xsl:variable>
        <xsl:message><xsl:value-of select="concat($type,' ---> ', $type2, ' ---> ', $r)"/></xsl:message>
        <xsl:value-of select="$r"/>
    </xsl:template>

    <xsl:template name="generateFunctionStub">
        <xsl:param name="pmode"/>
        <xsl:choose>
            <xsl:when test="@API='true' and ($file='' or location/@file=$file)">
                <xsl:variable name="c">
                    <xsl:call-template name="goCodeDriver">
                        <xsl:with-param name="pmode" select="$pmode"/>
                    </xsl:call-template>
                </xsl:variable>
                <xsl:choose>
                    <xsl:when test="contains($c,'FIXME')">
                        <xsl:if test="$mode='manual'"><xsl:value-of select="$c"/></xsl:if>
                    </xsl:when>
                    <xsl:when test="contains($c,'OBSOLETE') or contains($c,'not recommended')">
                        <xsl:message>
                            <xsl:value-of select="concat('skipping function: deprecation or recommendation found ',name)"/>
                        </xsl:message>
                    </xsl:when>
                    <xsl:when test="contains($c,'_private_ function')">
                        <xsl:message>
                            <xsl:value-of select="concat('skipping function: documented as _private_ ',name)"/>
                        </xsl:message>
                    </xsl:when>
                    <xsl:when test="contains($c,'IMPLOT_DEPRECATED')">
                        <xsl:message>
                            <xsl:value-of select="concat('skipping function: documented as IMPLOT_DEPRECATED ',name)"/>
                        </xsl:message>
                    </xsl:when>
                    <xsl:otherwise>
                        <xsl:if test="$mode='auto'"><xsl:value-of select="$c"/></xsl:if>
                    </xsl:otherwise>
                </xsl:choose>
            </xsl:when>
            <xsl:otherwise>
                <xsl:message><xsl:value-of select="concat('skipping function ',./definition,': @API=',@API,',location=',location/@file)"/></xsl:message>
            </xsl:otherwise>
        </xsl:choose>
    </xsl:template>

    <xsl:template match="/doxygen/compounddef/sectiondef/memberdef[@kind='function']">
        <xsl:call-template name="generateFunctionStub">
            <xsl:with-param name="pmode" select="'mandatory'" />
        </xsl:call-template>
        <xsl:call-template name="generateFunctionStub">
           <xsl:with-param name="pmode" select="'optional'" />
        </xsl:call-template>
    </xsl:template>

    <xsl:template match="text()"/>
</xsl:stylesheet>
