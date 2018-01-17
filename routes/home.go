// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package routes

import (
	"html"
	"fmt"
	"strings"
	"html/template"
	"regexp"
	"os"
	"os/exec"

	"github.com/Unknwon/paginater"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/pkg/context"
	"github.com/gogits/gogs/pkg/setting"
	"github.com/gogits/gogs/routes/user"
)

const (
	HOME                  = "home"
	EXPLORE_CODE          = "explore/code"
	EXPLORE_REPOS         = "explore/repos"
	EXPLORE_USERS         = "explore/users"
	EXPLORE_ORGANIZATIONS = "explore/organizations"
)

func Home(c *context.Context) {
	if c.IsLogged {
		if !c.User.IsActive && setting.Service.RegisterEmailConfirm {
			c.Data["Title"] = c.Tr("auth.active_your_account")
			c.Success(user.ACTIVATE)
		} else {
			user.Dashboard(c)
		}
		return
	}

	// Check auto-login.
	uname := c.GetCookie(setting.CookieUserName)
	if len(uname) != 0 {
		c.Redirect(setting.AppSubURL + "/user/login")
		return
	}

	c.Data["PageIsHome"] = true
	c.Success(HOME)
}

func ExploreRepos(c *context.Context) {
	c.Data["Title"] = c.Tr("explore")
	c.Data["PageIsExplore"] = true
	c.Data["PageIsExploreRepositories"] = true

	page := c.QueryInt("page")
	if page <= 0 {
		page = 1
	}

	keyword := c.Query("q")
	repos, count, err := models.SearchRepositoryByName(&models.SearchRepoOptions{
		Keyword:  keyword,
		UserID:   c.UserID(),
		OrderBy:  "updated_unix DESC",
		Page:     page,
		PageSize: setting.UI.ExplorePagingNum,
	})
	if err != nil {
		c.ServerError("SearchRepositoryByName", err)
		return
	}
	c.Data["Keyword"] = keyword
	c.Data["Total"] = count
	c.Data["Page"] = paginater.New(int(count), setting.UI.ExplorePagingNum, page, 5)

	if err = models.RepositoryList(repos).LoadAttributes(); err != nil {
		c.ServerError("RepositoryList.LoadAttributes", err)
		return
	}
	c.Data["Repos"] = repos

	c.Success(EXPLORE_REPOS)
}

type UserSearchOptions struct {
	Type     models.UserType
	Counter  func() int64
	Ranger   func(int, int) ([]*models.User, error)
	PageSize int
	OrderBy  string
	TplName  string
}

func RenderUserSearch(c *context.Context, opts *UserSearchOptions) {
	page := c.QueryInt("page")
	if page <= 1 {
		page = 1
	}

	var (
		users []*models.User
		count int64
		err   error
	)

	keyword := c.Query("q")
	if len(keyword) == 0 {
		users, err = opts.Ranger(page, opts.PageSize)
		if err != nil {
			c.ServerError("Ranger", err)
			return
		}
		count = opts.Counter()
	} else {
		users, count, err = models.SearchUserByName(&models.SearchUserOptions{
			Keyword:  keyword,
			Type:     opts.Type,
			OrderBy:  opts.OrderBy,
			Page:     page,
			PageSize: opts.PageSize,
		})
		if err != nil {
			c.ServerError("SearchUserByName", err)
			return
		}
	}
	c.Data["Keyword"] = keyword
	c.Data["Total"] = count
	c.Data["Page"] = paginater.New(int(count), opts.PageSize, page, 5)
	c.Data["Users"] = users

	c.Success(opts.TplName)
}

func ExploreUsers(c *context.Context) {
	c.Data["Title"] = c.Tr("explore")
	c.Data["PageIsExplore"] = true
	c.Data["PageIsExploreUsers"] = true

	RenderUserSearch(c, &UserSearchOptions{
		Type:     models.USER_TYPE_INDIVIDUAL,
		Counter:  models.CountUsers,
		Ranger:   models.Users,
		PageSize: setting.UI.ExplorePagingNum,
		OrderBy:  "updated_unix DESC",
		TplName:  EXPLORE_USERS,
	})
}

func ExploreOrganizations(c *context.Context) {
	c.Data["Title"] = c.Tr("explore")
	c.Data["PageIsExplore"] = true
	c.Data["PageIsExploreOrganizations"] = true

	RenderUserSearch(c, &UserSearchOptions{
		Type:     models.USER_TYPE_ORGANIZATION,
		Counter:  models.CountOrganizations,
		Ranger:   models.Organizations,
		PageSize: setting.UI.ExplorePagingNum,
		OrderBy:  "updated_unix DESC",
		TplName:  EXPLORE_ORGANIZATIONS,
	})
}

func ExploreCode(ctx *context.Context) {
  ctx.Data["Title"] = "CodeSearch"
  ctx.Data["PageIsExplore"] = true
  ctx.Data["PageIsExploreCode"] = true

  keyword := ctx.Query("q")
  extname := ctx.Query("ext")

  if ctx.IsLogged && len(keyword) > 0 {

    // get workdir. we need it to determine where to call the script.
    workDir, err := setting.WorkDir()
    if err != nil {
      fmt.Fprintln(os.Stderr, "Failed to get WorkDir: ", err)
      ctx.Handle(500, "No WorkDir.", err)
      return
    }

    // store full query for displaying in input later
    ctx.Data["Keyword"] = keyword

    // if query contains 'ext:foo', set foo as extension and remove from keyword
    if strings.Contains(keyword, "ext:") {
      re := regexp.MustCompile("ext:([a-z0-9]{1,5})")
      extname = strings.Replace(re.FindString(keyword), "ext:", "", 1)
      keyword = strings.Trim(re.ReplaceAllString(keyword, ""), " ")
    }

    var (
      cmdOut []byte
      boom   error
    )

    cmdName   := strings.Join([]string{workDir, "scripts", "searchcode"}, "/")
    reposPath := strings.Join([]string{setting.RepoRootPath, ctx.User.Name}, "/")
    cmdArgs   := []string{keyword, reposPath, extname}

    if cmdOut, boom = exec.Command(cmdName, cmdArgs...).Output(); boom != nil {
      fmt.Fprintln(os.Stderr, "There was an error running codesearch command: ", boom)
      ctx.Handle(500, "Error running codesearch command.", boom)
      return
    }

    result := string(cmdOut)
    if len(result) > 5 {

      output := strings.Join([]string{"\n", html.EscapeString(result), "</pre></div>"}, "")

      reg, err := regexp.Compile("\n([0-9]+).")
      if err != nil {
        fmt.Fprintln(os.Stderr, "There was an error compiling the regex: ", err)
        ctx.Handle(500, "Invalid regex.", err)
        return
      }

      output = reg.ReplaceAllString(output, "\n<span>$1</span> ")

      reg, err = regexp.Compile("\n([a-z0-9_\\.-]+): (.+)\n<span>([0-9]+)")
      if err != nil {
        fmt.Fprintln(os.Stderr, "There was an error compiling the regex: ", err)
        ctx.Handle(500, "Invalid regex.", err)
        return
      }

      output = reg.ReplaceAllString(output, "</pre></div>\n<div class='codesearch'>\n<a href='/{USERNAME}/$1'>$1</a> -- <a href='/{USERNAME}/$1/src/master/$2#L$3'>$2</a>\n<pre class='raw'><span>$3")

      output = strings.Replace(output, "{USERNAME}", ctx.User.Name, -1) // replace {USERNAME} with real username
      output = strings.Replace(output, "</pre></div>", "", 1) // remove first occurence
      output = strings.Replace(output, "--</pre>", "</pre>", -1) // remove trailing --'s
      output = strings.Replace(output, "\n--\n", "\n<span>--</span>\n", -1) // and wrap remaining --'s in span elements

      ctx.Data["Output"]  = template.HTML(output)

    } else {
      ctx.Data["Output"] = ""
    }

  } else {
    ctx.Data["Output"] = ""
  }

  // fmt.Fprintln(os.Stdout, result)
  ctx.HTML(200, EXPLORE_CODE)
}


func NotFound(c *context.Context) {
	c.Data["Title"] = "Page Not Found"
	c.NotFound()
}
