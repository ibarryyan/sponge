package generate

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"github.com/zhufuyi/sponge/pkg/replacer"
	"github.com/zhufuyi/sponge/pkg/sql2code"
	"github.com/zhufuyi/sponge/pkg/sql2code/parser"

	"github.com/huandu/xstrings"
	"github.com/spf13/cobra"
)

// HTTPCommand generate web service code
func HTTPCommand() *cobra.Command {
	var (
		moduleName  string // module name for go.mod
		serverName  string // server name
		projectName string // project name for deployment name
		repoAddr    string // image repo address
		outPath     string // output directory
		dbTables    string // table names
		sqlArgs     = sql2code.Args{
			Package:  "model",
			JSONTag:  true,
			GormType: true,
		}
	)

	//nolint
	cmd := &cobra.Command{
		Use:   "http",
		Short: "Generate web service code based on sql",
		Long: `generate web service code based on sql.

Examples:
  # generate web service code and embed gorm.model struct.
  sponge web http --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user

  # generate web service code, structure fields correspond to the column names of the table.
  sponge web http --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --embed=false

  # generate web service code with multiple table names.
  sponge web http --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=t1,t2

  # generate web service code and specify the output directory, Note: code generation will be canceled when the latest generated file already exists.
  sponge web http --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --out=./yourServerDir

  # generate web service code and specify the docker image repository address.
  sponge web http --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --repo-addr=192.168.3.37:9443/user-name --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user
`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var firstTable string
			var handlerTableNames []string
			tableNames := strings.Split(dbTables, ",")
			if len(tableNames) == 1 {
				firstTable = tableNames[0]
			} else if len(tableNames) > 1 {
				firstTable = tableNames[0]
				handlerTableNames = tableNames[1:]
			}

			projectName, serverName = convertProjectAndServerName(projectName, serverName)
			if sqlArgs.DBDriver == DBDriverMongodb {
				sqlArgs.IsEmbed = false
			}

			sqlArgs.DBTable = firstTable
			codes, err := sql2code.Generate(&sqlArgs)
			if err != nil {
				return err
			}
			g := &httpGenerator{
				moduleName:  moduleName,
				serverName:  serverName,
				projectName: projectName,
				repoAddr:    repoAddr,
				dbDSN:       sqlArgs.DBDsn,
				dbDriver:    sqlArgs.DBDriver,
				codes:       codes,
				outPath:     outPath,
			}
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			for _, handlerTableName := range handlerTableNames {
				if handlerTableName == "" {
					continue
				}

				sqlArgs.DBTable = handlerTableName
				codes, err := sql2code.Generate(&sqlArgs)
				if err != nil {
					return err
				}

				hg := &handlerGenerator{
					moduleName: moduleName,
					dbDriver:   sqlArgs.DBDriver,
					codes:      codes,
					outPath:    outPath,
				}
				outPath, err = hg.generateCode()
				if err != nil {
					return err
				}
			}

			fmt.Printf(`
using help:
  1. open a terminal and execute the command to generate the swagger documentation: make docs
  2. compile and run service: make run
  3. visit http://localhost:8080/swagger/index.html in your browser, and test the CRUD api interface.

`)
			fmt.Printf("generate %s's web service code successfully, out = %s\n", serverName, outPath)

			_ = generateConfigmap(serverName, outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "module-name is the name of the module in the go.mod file")
	_ = cmd.MarkFlagRequired("module-name")
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "server name")
	_ = cmd.MarkFlagRequired("server-name")
	cmd.Flags().StringVarP(&projectName, "project-name", "p", "", "project name")
	_ = cmd.MarkFlagRequired("project-name")
	cmd.Flags().StringVarP(&sqlArgs.DBDriver, "db-driver", "k", "mysql", "database driver, support mysql, mongodb, postgresql, tidb, sqlite")
	cmd.Flags().StringVarP(&sqlArgs.DBDsn, "db-dsn", "d", "", "database content address, e.g. user:password@(host:port)/database. Note: if db-driver=sqlite, db-dsn must be a local sqlite db file, e.g. --db-dsn=/tmp/sponge_sqlite.db") //nolint
	_ = cmd.MarkFlagRequired("db-dsn")
	cmd.Flags().StringVarP(&dbTables, "db-table", "t", "", "table name, multiple names separated by commas")
	_ = cmd.MarkFlagRequired("db-table")
	cmd.Flags().BoolVarP(&sqlArgs.IsEmbed, "embed", "e", true, "whether to embed gorm.model struct")
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "json tags name type, 0:snake case, 1:camel case")
	cmd.Flags().StringVarP(&repoAddr, "repo-addr", "r", "", "docker image repository address, excluding http and repository names")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output directory, default is ./serverName_http_<time>")

	return cmd
}

type httpGenerator struct {
	moduleName  string
	serverName  string
	projectName string
	repoAddr    string
	dbDSN       string
	dbDriver    string
	codes       map[string]string
	outPath     string
}

func (g *httpGenerator) generateCode() (string, error) {
	subTplName := "http"
	r := Replacers[TplNameSponge]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// setting up template information
	subDirs := []string{ // specify the subdirectory for processing
		"cmd/serverNameExample_httpExample", "sponge/configs", "sponge/deployments",
		"sponge/docs", "sponge/scripts", "sponge/internal",
	}
	subFiles := []string{ // specify the sub-documents to be processed
		"sponge/.gitignore", "sponge/.golangci.yml", "sponge/go.mod", "sponge/go.sum",
		"sponge/Jenkinsfile", "sponge/Makefile-for-http", "sponge/README.md",
	}
	ignoreDirs := []string{ // specify the directory in the subdirectory where processing is ignored
		"internal/service", "internal/rpcclient", "cmd/sponge",
	}
	var ignoreFiles []string
	switch strings.ToLower(g.dbDriver) {
	case DBDriverMysql, DBDriverPostgresql, DBDriverTidb, DBDriverSqlite:
		ignoreFiles = []string{ // specify the files in the subdirectory to be ignored for processing
			"swagger.json", "swagger.yaml", "apis.swagger.json", "apis.html", "apis.go", // sponge/docs
			"userExample_rpc.go", "systemCode_rpc.go", // internal/ecode
			"routers_pbExample.go", "routers_pbExample_test.go", "userExample_router.go", // internal/routers
			"grpc.go", "grpc_option.go", "grpc_test.go", // internal/server
			"scripts/image-rpc-test.sh", "scripts/patch.sh", "scripts/protoc.sh", "scripts/proto-doc.sh", // sponge/scripts
			"init_test.go", "init.go.mgo", // model
			"doc.go", "cacheNameExample.go", "cacheNameExample_test.go", "cache/userExample.go.mgo", // internal/cache
			"dao/userExample.go.mgo",                                                                                                              // internal/dao
			"handler/userExample_logic.go", "handler/userExample_logic_test.go", "handler/userExample.go.mgo", "handler/userExample_logic.go.mgo", // internal/handler
			"userExample_types.go.mgo", // internal/types
		}
	case DBDriverMongodb:
		ignoreFiles = []string{ // specify the files in the subdirectory to be ignored for processing
			"swagger.json", "swagger.yaml", "apis.swagger.json", "apis.html", "apis.go", // sponge/docs
			"userExample_rpc.go", "systemCode_rpc.go", // internal/ecode
			"routers_pbExample.go", "routers_pbExample_test.go", "userExample_router.go", // internal/routers
			"grpc.go", "grpc_option.go", "grpc_test.go", // internal/server
			"scripts/image-rpc-test.sh", "scripts/patch.sh", "scripts/protoc.sh", "scripts/proto-doc.sh", // sponge/scripts
			"init_test.go", "init.go", // model
			"doc.go", "cacheNameExample.go", "cacheNameExample_test.go", "cache/userExample.go", "cache/userExample_test.go", // internal/cache
			"dao/userExample_test.go", "dao/userExample.go", // internal/dao
			"handler/userExample_logic.go", "handler/userExample_logic_test.go", "handler/userExample.go", "handler/userExample_test.go", "handler/userExample_logic.go.mgo", // internal/handler
			"userExample_types.go", // internal/types
		}
	default:
		return "", errors.New("unsupported db driver: " + g.dbDriver)
	}

	r.SetSubDirsAndFiles(subDirs, subFiles...)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
	_ = r.SetOutputDir(g.outPath, g.serverName+"_"+subTplName)
	if err := r.SaveFiles(); err != nil {
		return "", err
	}
	_ = saveGenInfo(g.moduleName, g.serverName, r.GetOutputDir())

	return r.GetOutputDir(), nil
}

func (g *httpGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	repoHost, _ := parseImageRepoAddr(g.repoAddr)

	fields = append(fields, deleteFieldsMark(r, modelFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, modelInitDBFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoMgoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoTestFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, handlerFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, handlerMgoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, handlerTestFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, httpFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, dockerFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, dockerFileBuild, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, dockerComposeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, k8sDeploymentFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, k8sServiceFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, imageBuildFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, imageBuildLocalFile, wellStartMark, wellEndMark)...)
	//fields = append(fields, deleteAllFieldsMark(r, makeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, gitIgnoreFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteAllFieldsMark(r, protoShellFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteAllFieldsMark(r, appConfigFile, wellStartMark, wellEndMark)...)
	//fields = append(fields, deleteFieldsMark(r, deploymentConfigFile, wellStartMark, wellEndMark)...)
	fields = append(fields, replaceFileContentMark(r, readmeFile, "## "+g.serverName)...)
	fields = append(fields, []replacer.Field{
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark,
			New: httpServerConfigCode,
		},
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark2,
			New: getDBConfigCode(g.dbDriver),
		},
		{ // replace the contents of the model/userExample.go file
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{ // replace the contents of the model/init.go file
			Old: modelInitDBFileMark,
			New: getInitDBCode(g.dbDriver),
		},
		{ // replace the contents of the dao/userExample.go file
			Old: daoFileMark,
			New: g.codes[parser.CodeTypeDAO],
		},
		{ // replace the contents of the handler/userExample.go file
			Old: handlerFileMark,
			New: adjustmentOfIDType(g.codes[parser.CodeTypeHandler], g.dbDriver),
		},
		{ // replace the contents of the Dockerfile file
			Old: dockerFileMark,
			New: dockerFileHTTPCode,
		},
		{ // replace the contents of the Dockerfile_build file
			Old: dockerFileBuildMark,
			New: dockerFileBuildHTTPCode,
		},
		{ // replace the contents of the image-build.sh file
			Old: imageBuildFileMark,
			New: imageBuildFileHTTPCode,
		},
		{ // replace the contents of the image-build-local.sh file
			Old: imageBuildLocalFileMark,
			New: imageBuildLocalFileHTTPCode,
		},
		{ // replace the contents of the docker-compose.yml file
			Old: dockerComposeFileMark,
			New: dockerComposeFileHTTPCode,
		},
		//{ // replace the contents of the *-configmap.yml file
		//	Old: deploymentConfigFileMark,
		//	New: getDBConfigCode(g.dbDriver, true),
		//},
		{ // replace the contents of the *-deployment.yml file
			Old: k8sDeploymentFileMark,
			New: k8sDeploymentFileHTTPCode,
		},
		{ // replace the contents of the *-svc.yml file
			Old: k8sServiceFileMark,
			New: k8sServiceFileHTTPCode,
		},
		{ // replace github.com/zhufuyi/sponge/templates/sponge
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		{
			Old: protoShellFileGRPCMark,
			New: "",
		},
		{
			Old: protoShellFileMark,
			New: "",
		},
		{
			Old: "github.com/zhufuyi/sponge",
			New: g.moduleName,
		},
		{
			Old: g.moduleName + "/pkg",
			New: "github.com/zhufuyi/sponge/pkg",
		},
		{ // replace the sponge version of the go.mod file
			Old: spongeTemplateVersionMark,
			New: getLocalSpongeTemplateVersion(),
		},
		{
			Old: "sponge api docs",
			New: g.serverName + " api docs",
		},
		{
			Old: "userExampleNO       = 1",
			New: fmt.Sprintf("userExampleNO = %d", rand.Intn(100)),
		},
		{
			Old: "serverNameExample",
			New: g.serverName,
		},
		// docker image and k8s deployment script replacement
		{
			Old: "server-name-example",
			New: xstrings.ToKebabCase(g.serverName), // snake_case to kebab_case
		},
		// docker image and k8s deployment script replacement
		{
			Old: "project-name-example",
			New: g.projectName,
		},
		{
			Old: "projectNameExample",
			New: g.projectName,
		},
		{
			Old: "repo-addr-example",
			New: g.repoAddr,
		},
		{
			Old: "image-repo-host",
			New: repoHost,
		},
		{
			Old: "_httpExample",
			New: "",
		},
		{
			Old: "_mixExample",
			New: "",
		},
		{
			Old: "_pbExample",
			New: "",
		},
		{
			Old: "root:123456@(192.168.3.37:3306)/account",
			New: g.dbDSN,
		},
		{
			Old: "root:123456@192.168.3.37:27017/account",
			New: g.dbDSN,
		},
		{
			Old: "root:123456@192.168.3.37:5432/account",
			New: g.dbDSN,
		},
		{
			Old: "test/sql/sqlite/sponge.db",
			New: sqliteDSNAdaptation(g.dbDriver, g.dbDSN),
		},
		{
			Old: "Makefile-for-http",
			New: "Makefile",
		},
		{
			Old: "init.go.mgo",
			New: "init.go",
		},
		{
			Old: "userExample_types.go.mgo",
			New: "userExample_types.go",
		},
		{
			Old: "userExample.go.mgo",
			New: "userExample.go",
		},
		{
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	return fields
}
