package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	wxsg "github.com/reveever/go-weixin-sogou"
	"github.com/xuri/excelize/v2"
)

const (
	targetAccount = "ConnectEd"
	outputFile    = "ConnectEd_articles.xlsx"
	sheetJobs     = "招聘机会"
	sheetAll      = "全部文章"
)

// jobKeywords are checked against title + preview (case-sensitive for English, case-insensitive not needed since Chinese is exact)
var jobKeywords = []string{
	"招RA", "招聘", "PhD", "博士后", "博士",
	"Postdoc", "postdoc", "Research Assistant", "research assistant",
	"Position", "position", "Opening", "opening", "Hiring", "hiring",
	"Fellowship", "fellowship", "Internship", "internship",
}

// searchQueries drives what we ask Sogou for; all results are filtered to targetAccount
var searchQueries = []string{
	targetAccount,
	targetAccount + " 招聘",
	targetAccount + " PhD",
	targetAccount + " 博士",
	targetAccount + " 博士后",
	targetAccount + " RA",
	targetAccount + " Postdoc",
	targetAccount + " Fellowship",
}

// ArticleRecord is a denormalized row for the Excel output
type ArticleRecord struct {
	Title    string
	URL      string
	AccName  string
	PubTime  time.Time
	Preview  string
	Keywords []string // matched job keywords
}

func main() {
	refresh := flag.Bool("refresh", false, "只追加最新文章，跳过 Excel 中已有的条目")
	maxPages := flag.Int("pages", 10, "每个搜索词最多翻页数")
	out := flag.String("out", outputFile, "输出 Excel 文件路径")
	debug := flag.Bool("debug", false, "打印所有搜索结果（不过滤公众号名），用于排查账号名")
	flag.Parse()

	if *debug {
		runDebug(*maxPages)
		return
	}

	existingURLs := map[string]bool{}
	if *refresh {
		if _, err := os.Stat(*out); err == nil {
			existingURLs = loadExistingURLs(*out)
			fmt.Printf("[刷新模式] 已有 %d 条记录，只追加新内容\n", len(existingURLs))
		}
	}

	fmt.Printf("正在搜索公众号「%s」的文章（最多 %d 页/关键词）…\n\n", targetAccount, *maxPages)
	newRecords := collectArticles(*maxPages, existingURLs, *refresh)

	if len(newRecords) == 0 {
		if *refresh {
			fmt.Println("没有发现新文章，Excel 无需更新。")
		} else {
			fmt.Println("未搜索到任何文章，请检查公众号名称或网络。")
		}
		return
	}

	fmt.Printf("\n共找到 %d 篇新文章，正在写入 %s …\n", len(newRecords), *out)
	if err := writeExcel(*out, newRecords, *refresh); err != nil {
		log.Fatalf("写入 Excel 失败: %v", err)
	}

	jobCount := 0
	for _, r := range newRecords {
		if len(r.Keywords) > 0 {
			jobCount++
		}
	}
	fmt.Printf("完成！招聘相关文章: %d 篇 / 全部新增: %d 篇\n", jobCount, len(newRecords))
	fmt.Printf("文件已保存: %s\n", *out)
}

// runDebug prints raw Sogou results without any account-name filter.
func runDebug(maxPages int) {
	queries := []string{targetAccount, targetAccount + " 招聘", targetAccount + " PhD"}
	for _, query := range queries {
		fmt.Printf("\n=== 搜索: %q ===\n", query)
		for page := 1; page <= maxPages; page++ {
			results, err := wxsg.SearchArticle(query, page)
			if err != nil {
				fmt.Printf("  第 %d 页: 错误 %v\n", page, err)
				break
			}
			if len(results) == 0 {
				fmt.Printf("  第 %d 页: 无结果\n", page)
				break
			}
			for _, r := range results {
				fmt.Printf("  AccName=%q  Title=%q\n", r.AccName, r.Title)
			}
			break // only page 1 in debug mode
		}
	}

	fmt.Println("\n=== SearchAccount ===")
	accounts, err := wxsg.SearchAccount(targetAccount, 1)
	if err != nil {
		fmt.Printf("  错误: %v\n", err)
		return
	}
	for _, a := range accounts {
		fmt.Printf("  Name=%q  WeixinID=%q  Latest=%q\n", a.Name, a.WeixinID, a.LatestArticleTitle)
	}
}

// collectArticles queries Sogou across multiple search terms and deduplicates by URL.
// If stopURLs is non-empty (refresh mode), articles whose URL is already known are skipped.
// isRefresh controls whether to stop early when a page yields no new articles.
func collectArticles(maxPages int, stopURLs map[string]bool, isRefresh bool) []ArticleRecord {
	seen := map[string]bool{}
	var records []ArticleRecord

	for _, query := range searchQueries {
		fmt.Printf("  → 搜索: %q\n", query)
		for page := 1; page <= maxPages; page++ {
			results, err := wxsg.SearchArticle(query, page)
			if err != nil {
				if strings.Contains(err.Error(), "no results") {
					break
				}
				fmt.Printf("    第 %d 页出错: %v，跳过\n", page, err)
				time.Sleep(3 * time.Second)
				break
			}

			pageNewCount := 0
			for _, r := range results {
				// Exact case-sensitive match on account name
				if r.AccName != targetAccount {
					continue
				}
				if seen[r.Url] || stopURLs[r.Url] {
					continue
				}
				seen[r.Url] = true
				pageNewCount++

				rec := ArticleRecord{
					Title:    r.Title,
					URL:      r.Url,
					AccName:  r.AccName,
					PubTime:  r.PubTime,
					Preview:  r.Preview,
					Keywords: matchKeywords(r.Title, r.Preview),
				}
				records = append(records, rec)
			}

			fmt.Printf("    第 %d 页: +%d 篇\n", page, pageNewCount)

			// In refresh mode, stop early once a full page has nothing new
			if isRefresh && pageNewCount == 0 {
				break
			}

			time.Sleep(1200 * time.Millisecond) // polite delay
		}
	}

	// Sort newest first (zero PubTime goes last)
	sort.Slice(records, func(i, j int) bool {
		if records[i].PubTime.IsZero() {
			return false
		}
		if records[j].PubTime.IsZero() {
			return true
		}
		return records[i].PubTime.After(records[j].PubTime)
	})

	return records
}

func matchKeywords(title, preview string) []string {
	text := title + " " + preview
	seen := map[string]bool{}
	var matched []string
	for _, kw := range jobKeywords {
		if strings.Contains(text, kw) && !seen[kw] {
			matched = append(matched, kw)
			seen[kw] = true
		}
	}
	return matched
}

// ── Excel helpers ────────────────────────────────────────────────────────────

var (
	headerStyle  int
	jobRowStyle  int
	dateStyle    int
	linkStyle    int
	normalStyle  int
)

func loadExistingURLs(filename string) map[string]bool {
	f, err := excelize.OpenFile(filename)
	if err != nil {
		return map[string]bool{}
	}
	defer f.Close()

	urls := map[string]bool{}
	rows, _ := f.GetRows(sheetAll)
	for i, row := range rows {
		if i == 0 {
			continue // header
		}
		// Column F (index 5) = URL
		if len(row) >= 6 {
			if u := strings.TrimSpace(row[5]); u != "" {
				urls[u] = true
			}
		}
	}
	return urls
}

func writeExcel(filename string, newRecords []ArticleRecord, appendMode bool) error {
	var f *excelize.File
	var existingJobRows [][]string
	var existingAllRows [][]string

	if appendMode {
		opened, err := excelize.OpenFile(filename)
		if err == nil {
			existingJobRows = dataRows(opened, sheetJobs)
			existingAllRows = dataRows(opened, sheetAll)
			opened.Close()
		}
		f = excelize.NewFile()
	} else {
		f = excelize.NewFile()
	}

	// Build styles
	hs, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F4E79"}, Pattern: 1},
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thinBorder(),
	})
	headerStyle = hs

	js, _ := f.NewStyle(&excelize.Style{
		Fill:   excelize.Fill{Type: "pattern", Color: []string{"FFF2CC"}, Pattern: 1},
		Border: thinBorder(),
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	jobRowStyle = js

	ds, _ := f.NewStyle(&excelize.Style{
		Border:    thinBorder(),
		NumFmt:    14, // yyyy-mm-dd
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	dateStyle = ds

	ls, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Color: "0563C1", Underline: "single"},
		Border: thinBorder(),
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	linkStyle = ls

	ns, _ := f.NewStyle(&excelize.Style{
		Border: thinBorder(),
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	normalStyle = ns

	// ── Sheet: 招聘机会 ──────────────────────────────────────────────────────
	f.NewSheet(sheetJobs)
	writeSheetHeader(f, sheetJobs)

	rowNum := 2
	for _, rec := range newRecords {
		if len(rec.Keywords) > 0 {
			writeRow(f, sheetJobs, rowNum, rec, true)
			rowNum++
		}
	}
	for _, row := range existingJobRows {
		writeRawRow(f, sheetJobs, rowNum, row)
		rowNum++
	}
	applySheetFormatting(f, sheetJobs, rowNum-1)

	// ── Sheet: 全部文章 ──────────────────────────────────────────────────────
	f.NewSheet(sheetAll)
	writeSheetHeader(f, sheetAll)

	rowNum = 2
	for _, rec := range newRecords {
		writeRow(f, sheetAll, rowNum, rec, false)
		rowNum++
	}
	for _, row := range existingAllRows {
		writeRawRow(f, sheetAll, rowNum, row)
		rowNum++
	}
	applySheetFormatting(f, sheetAll, rowNum-1)

	// Remove default Sheet1
	f.DeleteSheet("Sheet1")

	// Make 招聘机会 the active sheet
	idx, _ := f.GetSheetIndex(sheetJobs)
	f.SetActiveSheet(idx)

	return f.SaveAs(filename)
}

// columns: 序号 | 标题 | 公众号 | 发布日期 | 关键词 | 文章链接 | 摘要
var headers = []string{"序号", "标题", "公众号", "发布日期", "关键词匹配", "文章链接", "摘要"}
var colWidths = []float64{6, 45, 12, 14, 22, 50, 40}

func writeSheetHeader(f *excelize.File, sheet string) {
	cols := []string{"A", "B", "C", "D", "E", "F", "G"}
	for i, h := range headers {
		cell := cols[i] + "1"
		f.SetCellValue(sheet, cell, h)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
		f.SetColWidth(sheet, cols[i], cols[i], colWidths[i])
	}
	f.SetRowHeight(sheet, 1, 30)
	f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
}

func writeRow(f *excelize.File, sheet string, rowNum int, rec ArticleRecord, isJob bool) {
	rowStr := fmt.Sprintf("%d", rowNum)
	style := normalStyle
	if isJob {
		style = jobRowStyle
	}

	f.SetCellValue(sheet, "A"+rowStr, rowNum-1)
	f.SetCellStyle(sheet, "A"+rowStr, "A"+rowStr, style)

	f.SetCellValue(sheet, "B"+rowStr, rec.Title)
	f.SetCellStyle(sheet, "B"+rowStr, "B"+rowStr, style)

	f.SetCellValue(sheet, "C"+rowStr, rec.AccName)
	f.SetCellStyle(sheet, "C"+rowStr, "C"+rowStr, style)

	if !rec.PubTime.IsZero() {
		f.SetCellValue(sheet, "D"+rowStr, rec.PubTime)
		f.SetCellStyle(sheet, "D"+rowStr, "D"+rowStr, dateStyle)
	} else {
		f.SetCellValue(sheet, "D"+rowStr, "")
		f.SetCellStyle(sheet, "D"+rowStr, "D"+rowStr, style)
	}

	f.SetCellValue(sheet, "E"+rowStr, strings.Join(rec.Keywords, ", "))
	f.SetCellStyle(sheet, "E"+rowStr, "E"+rowStr, style)

	f.SetCellValue(sheet, "F"+rowStr, rec.URL)
	if err := f.SetCellHyperLink(sheet, "F"+rowStr, rec.URL, "External"); err == nil {
		f.SetCellStyle(sheet, "F"+rowStr, "F"+rowStr, linkStyle)
	} else {
		f.SetCellStyle(sheet, "F"+rowStr, "F"+rowStr, style)
	}

	f.SetCellValue(sheet, "G"+rowStr, rec.Preview)
	f.SetCellStyle(sheet, "G"+rowStr, "G"+rowStr, style)

	f.SetRowHeight(sheet, rowNum, 20)
}

// writeRawRow re-writes a row that was loaded from an existing sheet (string slice)
func writeRawRow(f *excelize.File, sheet string, rowNum int, row []string) {
	cols := []string{"A", "B", "C", "D", "E", "F", "G"}
	rowStr := fmt.Sprintf("%d", rowNum)
	for i, col := range cols {
		val := ""
		if i < len(row) {
			val = row[i]
		}
		cell := col + rowStr
		f.SetCellValue(sheet, cell, val)
		if col == "F" && val != "" {
			if err := f.SetCellHyperLink(sheet, cell, val, "External"); err == nil {
				f.SetCellStyle(sheet, cell, cell, linkStyle)
				continue
			}
		}
		f.SetCellStyle(sheet, cell, cell, normalStyle)
	}
	f.SetRowHeight(sheet, rowNum, 20)
}

func applySheetFormatting(f *excelize.File, sheet string, lastRow int) {
	if lastRow < 2 {
		return
	}
	// Auto-filter on header row
	endCol := "G"
	f.AutoFilter(sheet, "A1:"+endCol+fmt.Sprintf("%d", lastRow), []excelize.AutoFilterOptions{})
}

func dataRows(f *excelize.File, sheet string) [][]string {
	rows, _ := f.GetRows(sheet)
	if len(rows) <= 1 {
		return nil
	}
	return rows[1:] // skip header
}

func thinBorder() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "D9D9D9", Style: 1},
		{Type: "right", Color: "D9D9D9", Style: 1},
		{Type: "top", Color: "D9D9D9", Style: 1},
		{Type: "bottom", Color: "D9D9D9", Style: 1},
	}
}
